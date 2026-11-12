// w3_reader_surfaces_test.go — W3-4's WEB readers (picker, C-9 display,
// intake's unconfirmed clearing — each resolving through the ONE composed
// path, on fixtures where a second path would differ), W3-7 (the muster
// never gains alias resolution OF PEOPLE), and W3-2's HTTP half (a
// refused POST is pre-append). design/WINDOW-BUILD-PLAN-v1.md §3 W3,
// criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANTS: m9 (a reader resolving via a second/stale path — the
// legacy-ref_alias intake fixture), m14 (zero composition surfacing as
// stale names on picker/roster/muster), m4 (the muster resolving PEOPLE
// through a merge path — doubles as W3-7's control, the ledger pairing),
// m6 (mint-then-refuse at the HTTP edge).
//
// The reader enumeration is HAND-KEPT (the criterion's own honesty
// clause): picker + C-9 display here are exercised on the composed-NAME
// fixture (their output is names; base-row-vs-composed IS their
// second-path-differs case); the same_as-dependent readers (intake's
// clearing, the merge map, composed person, dump person data) are
// exercised on the contested-merge and divergence fixtures here and in
// internal/domain/w3_merge_readers_test.go.
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w3PageHas reports whether the operator-visible markup contains needle
// (case-insensitive, style/script stripped).
func w3PageHas(page, needle string) bool {
	return strings.Contains(w3PageText(page), strings.ToLower(needle))
}

func w3Get200(t *testing.T, srv *Server, path string) string {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	rec := w2Get(t, srv, path, cookie)
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestW3ReadersShowComposedName — W3-4 for the display readers: after a
// name amendment, the picker (/gate's roster), the roster page, and the
// LIVE muster (C-9's display) all show the COMPOSED name and never the
// base row's — the base row is the second path that would differ, and
// refdata.go:332/338 measurably returned it at the clean tree.
func TestW3ReadersShowComposedName(t *testing.T) {
	srv, st, x, _ := w3SiteAServer(t)
	// On site FIRST (the live board shows what is true NOW — schema-10
	// §5A.5(b): MusterLive reads the EFFECTIVE name).
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-31","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-31"),
		Driver:    domain.PersonSlot{ExistingID: x.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	fresh := "Freshly Amended Name"
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: x.PersonID, Name: &fresh}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	surfaces := []struct {
		path, label string
		raw         bool // the gate's roster rides an embedded JSON script block, which w2StripNonMarkup removes — search the raw body there
	}{
		{"/roster", "roster page", false},
		{"/gate", "picker (gate roster payload)", true},
		{"/muster", "live muster (C-9 display)", false},
	}
	for _, s := range surfaces {
		page := w3Get200(t, srv, s.path)
		text := w3PageText(page)
		if s.raw {
			text = strings.ToLower(page)
		}
		if !strings.Contains(text, strings.ToLower(fresh)) {
			t.Fatalf("W3-4/W3-8 (m14) on the %s: the composed name %q does not render — the reader is not on the ONE composed path", s.label, fresh)
		}
		if strings.Contains(text, strings.ToLower(x.Name)) {
			t.Fatalf("W3-4 (m9) on the %s: the BASE-ROW name %q still renders after the amendment — a reader answered from the second path", s.label, x.Name)
		}
	}
}

// TestW3IntakeUnconfirmedClearsThroughComposedSameAs — W3-4 for intake's
// unconfirmed clearing (P-3 §6: "derives from composed same_as; clearing
// routes through the amendment surface itself"): a record authored at the
// non-entrance post is flagged; the composed merge — authored on the
// contested-merge shape so the record-order winner is what clears it —
// removes the flag.
func TestW3IntakeUnconfirmedClearsThroughComposedSameAs(t *testing.T) {
	srv, st, x, _ := w3SiteAServer(t)
	// A person enrolled AT THE PAD (site-b) — Unconfirmed at the entrance.
	visitor := w3RemotePerson(t, st, "Pad Enrolled Visitor")

	page := w3Get200(t, srv, "/roster")
	if !w3PageHas(page, "Identity not yet confirmed") || !w3PageHas(page, "Pad Enrolled Visitor") {
		t.Fatalf("W3-4 fixture: the pad-enrolled record is not flagged Unconfirmed before any merge — the flag machinery is not live, so the clearing assertion below would be vacuous. Page:\n%s", page)
	}

	// The composed merge (the officer's reconciliation) clears it.
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: visitor, SameAs: &x.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	page = w3Get200(t, srv, "/roster")
	if w3PageHas(page, "Identity not yet confirmed") {
		t.Fatalf("W3-4: the Unconfirmed flag survives a COMPOSED same_as merge — intake's derivation is not reading the one composed path. Page:\n%s", page)
	}
}

// TestW3IntakeUnconfirmedIgnoresLegacyRefAlias — the second-path-would-
// differ fixture at its sharpest (m9): a LEGACY ref_alias row (the
// mechanism that used to clear this flag, alive in the tree until W8 —
// P-3 §5) touches the record, but NO composed same_as exists. The flag
// must STAND: person aliasing via ref_alias is superseded at W3, and an
// implementation still deriving Unconfirmed from ref_alias goes green
// here on the wrong path.
func TestW3IntakeUnconfirmedIgnoresLegacyRefAlias(t *testing.T) {
	// ⟦LOCK — §60, 2026-09-03⟧ KIND B (site 21) — DISPOSITION (3), THE DISCRIMINATION
	// RE-EXPRESSED AGAINST A SURVIVING RIVAL. Not retired: the rule this guards is alive
	// and a constructible rival still poses it.
	//
	// WHAT IT GUARDED: that Unconfirmed derives from the ONE composed path and nothing
	// else — "an implementation still deriving Unconfirmed from ref_alias goes green here
	// on the wrong path" (m9).
	// WHY THE OLD RIVAL IS GONE: P-3 §5 deletes the second mechanism, so a legacy row
	// cannot be authored and the fixture could not pose its case.
	// ⭐ THE SURVIVING RIVAL, AND IT IS A REAL RISK RATHER THAN A STAND-IN: intake.go:308-314
	// builds `reconciled` from `p.SameAs != nil` ALONE. So the rival is no longer a second
	// MECHANISM but a second READING of the same one — an implementation that clears the
	// flag because the record "has been amended" rather than because a composed same_as
	// exists. A non-same_as amendment poses exactly that, and it is authorable today.
	// ⚠ THE DISCRIMINATION IS UNCHANGED — the flag must derive from composed same_as and
	// from nothing else; only the rival that contests it has been re-pointed.
	//   ⟦was: "srv, st, x, _ := w3SiteAServer(t)"⟧
	//   ⟦was: "if _, err := domain.CreateAlias(st, \"site-a\", 1, visitor, x.PersonID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatalf(\"fixture: legacy ref_alias authoring failed: %v\", err)"⟧
	//   ⟦was: "}"⟧
	srv, st, _, _ := w3SiteAServer(t)
	visitor := w3RemotePerson(t, st, "Legacy Alias Visitor")

	// THE RIVAL: an amendment that TOUCHES the record but asserts no same_as.
	phone := "907-555-0142"
	if _, err := domain.AmendPerson(st, "site-a", 1,
		domain.PersonAmendment{PersonID: visitor, Phone: &phone}, 0, "default"); err != nil {
		t.Fatalf("fixture: the rival (non-same_as) amendment was refused: %v", err)
	}

	// Fixture-defect guard (rule 6): the rival must ACTUALLY have landed and must NOT have
	// produced a composed same_as — otherwise this test is green for the wrong reason.
	ps, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, q := range ps {
		if q.PersonID != visitor {
			continue
		}
		seen = true
		if q.Phone == nil || *q.Phone != phone {
			t.Fatalf("fixture defect: the rival amendment did not land (phone = %v) — the record is not amended at all, so this test poses nothing", q.Phone)
		}
		if q.SameAs != nil {
			t.Fatalf("fixture defect: the rival amendment produced a composed same_as (%q) — it must be a NON-merge amendment, or the flag is entitled to clear", *q.SameAs)
		}
	}
	if !seen {
		t.Fatalf("fixture defect: the remote visitor %s is not on the roster at all", visitor)
	}

	page := w3Get200(t, srv, "/roster")
	if !w3PageHas(page, "Identity not yet confirmed") || !w3PageHas(page, "Legacy Alias Visitor") {
		t.Fatalf("W3-4 (m9): an amendment carrying NO same_as CLEARED the Unconfirmed flag — intake is deriving the flag from \"this record has been amended\" rather than from the composed same_as it must read (intake.go:308-314; P-3 §6). Page:\n%s", page)
	}
}

// w3RemotePerson enrols a person at site-b and replicates the event in.
func w3RemotePerson(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	scratch := w3OpenRegistered(t, "w3-remote-person.db")
	p, err := domain.CreatePerson(scratch, "site-b", 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w3ApplyWire(t, st, w3Stream(t, scratch, "site-b", 1))
	return p.PersonID
}

// TestW3MusterNeverResolvesPeopleThroughMerges — W3-7 (D-28's safety
// clause; recount §6.5): a merge never changes who is shown on-site. The
// guard is PERSON-SCOPED: company/workgroup snapshot grouping through the
// alias map is sanctioned and untouched. m4 — the muster resolving the
// checked-in X into Y — is the named mutant AND this test's control (the
// ledger records the pairing; §5's CONTROL row).
func TestW3MusterNeverResolvesPeopleThroughMerges(t *testing.T) {
	srv, st, x, y := w3SiteAServer(t)
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-32","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-32"),
		Driver:    domain.PersonSlot{ExistingID: x.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// The merge: X (on site) same_as Y (never on site).
	if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	page := w3Get200(t, srv, "/muster")
	if !w3PageHas(page, x.Name) {
		t.Fatalf("W3-7 (m4): the checked-in person %q vanished from the muster after a merge — a merge changed who is shown on-site, on the life-safety document. Page:\n%s", x.Name, page)
	}
	if w3PageHas(page, y.Name) {
		t.Fatalf("W3-7 (m4): the merge target %q — never on site — appears on the muster; the muster resolved a PERSON through a merge path. A merge changes who is listed once, never who is shown on-site (D-28/D-46). Page:\n%s", y.Name, page)
	}
}

// TestW3RefusedAmendmentPostIsPreAppend — W3-2's HTTP half: a refused
// POST (name-to-empty) leaves the log untouched — no envelope of any kind
// is minted (m6: mint-then-refuse), and no PRG success is issued.
func TestW3RefusedAmendmentPostIsPreAppend(t *testing.T) {
	srv, st, x, _ := w3SiteAServer(t)
	before := w2Watermark(t, st, "site-a", 1)
	form := url.Values{}
	form.Set(w3FieldPersonID, x.PersonID)
	form.Set(w3FieldName, "")
	rec, _ := w3PostAmend(t, srv, form)
	if rec.Code == 303 && strings.Contains(rec.Result().Header.Get("Location"), "recorded=") {
		t.Fatalf("W3-2: the name-to-empty amendment was ACCEPTED (303 with ?recorded=) — this edge refusal's failure mode was never produced")
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W3-2 (m6): the refused POST grew the log (%d → %d) — refusals are PRE-APPEND input validation (VF §9 constraint 1)", before, after)
	}
}
