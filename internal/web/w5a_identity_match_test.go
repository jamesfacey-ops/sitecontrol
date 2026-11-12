// w5a_identity_match_test.go — W5a-6: identity matching compares the
// composed `Name` only, at EACH of the four measured person-name compare
// sites, and compares it with `strings.EqualFold(strings.TrimSpace(…))`.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// THE FOUR PERSON SITES, MEASURED at 88dd6a6 (the complete non-test
// EqualFold set is six: these four plus the company/workgroup twins at
// intake.go:462 and intake.go:494):
//
//	A. internal/web/intake.go:530  the intake duplicate refusal
//	B. internal/web/gate.go:839    resolveTypedPerson, the exact-match resolver
//	C. internal/web/gate.go:908    rowPersonSlot's deliberate off-roster add
//	D. internal/web/gate.go:924    rowPersonSlot honouring a picked id only
//	                               while it still agrees with the visible name
//
// ⚠ `gate.go:908` IS a person compare, not a company/workgroup twin — rev 6
// misclassified it and rev 7 corrected it (R2-W10). A test-writer working the
// hand-kept enumeration who skips :908 under-produces (m5a-6b), which is
// H13's defect reproduced by the edit that fixed H13's sibling. It is
// asserted below.
//
// ⟦NO PRE-CHUNK RED⟧ — the resolvers already compare `Name` only. **A clean
// pass here is not evidence.** The ledger row is the planted part-consulting
// resolver plus (m5a-6), and the dropped-EqualFold plant (m5a-6b) AT EACH OF
// THE FOUR SITES.
//
// KILLER MUTANTS:
//
//	(m5a-6)   identity/duplicate matching consulting LastName. Killed by the
//	          two-people-one-LastName fixture: consulting parts CHANGES the
//	          answer on it, by construction.
//	(m5a-6b)  EqualFold dropped from an exact-match resolver, TrimSpace kept.
//	          Killed at each site by a case-differing input whose two paths
//	          give observably different results.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// The fixture's three names. Mary and Yuki share a LAST NAME and have
// DIFFERENT composed Names — so a resolver that consulted the part would
// return a different answer, which is what makes (m5a-6) killable.
const (
	w5aIdMaryName  = "Ferguson, Mary"
	w5aIdYukiName  = "Nakamura, Yuki"
	w5aSharedLast  = "Ferguson"
	w5aIdMaryUpper = "FERGUSON, MARY"
)

type w5aIdentityWorld struct {
	srv       *Server
	companyID string
	maryID    string
	yukiID    string
}

// w5aIdentityWorld builds the non-degenerate identity fixture and asserts its
// own preconditions (rule 6).
func w5aBuildIdentityWorld(t *testing.T) w5aIdentityWorld {
	t.Helper()
	srv, st := w5aServer(t, "site-a", "")
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	mary := w5aPerson(t, st, "site-a", w5aIdMaryName, w5aSharedLast, &co.CompanyID, nil)
	yuki := w5aPerson(t, st, "site-a", w5aIdYukiName, w5aSharedLast, &co.CompanyID, nil)

	// Rule 6 — the arrangement this whole file needs: two people with the
	// SAME last_name and DIFFERENT composed Names, read back from the store.
	// Without it every "compares Name only" assertion is satisfied by a
	// resolver that compares the part, and the file proves nothing.
	for _, p := range w5aReadBack(t, st, mary.PersonID, yuki.PersonID) {
		if p.LastName == nil || *p.LastName != w5aSharedLast {
			t.Fatalf("fixture defect: %q read back with last_name %s, want %q — consulting the part must be able to CHANGE the answer", p.Name, w5aShowPtr(p.LastName), w5aSharedLast)
		}
	}
	if w5aIdMaryName == w5aIdYukiName {
		t.Fatal("fixture defect: the two fixture people share a composed Name")
	}
	return w5aIdentityWorld{srv: srv, companyID: co.CompanyID, maryID: mary.PersonID, yukiID: yuki.PersonID}
}

func w5aShowPtr(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return `"` + *p + `"`
}

// =======================================================================
// SITE A — intake.go:530, the duplicate refusal
// =======================================================================

func TestW5aIntakeDuplicateCheckComparesComposedNameOnly(t *testing.T) {
	w := w5aBuildIdentityWorld(t)

	// (m5a-6b): a name typed in a different case still resolves to the
	// person. Without EqualFold this enrols a second identity for one human
	// — the one error the alias machinery cannot repair, because MusterLive
	// alias-resolves company and workgroup ids and NEVER person ids.
	rec := postForm(t, w.srv, "/roster/intake", intakeForm2("FERGUSON", "MARY", w.companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Errorf("W5a-6 (m5a-6b) at intake.go:530: enrolling %q against an existing %q returned %d, want 409 — the compare is strings.EqualFold(strings.TrimSpace(…)) on Name, and a mutant dropping EqualFold while keeping TrimSpace mints a second record for one person", w5aIdMaryUpper, w5aIdMaryName, rec.Code)
	}

	// TrimSpace, the other half of the pinned rule.
	rec = postForm(t, w.srv, "/roster/intake", intakeForm2("  Ferguson  ", "  Mary  ", w.companyID, "", "", ""))
	if rec.Code != http.StatusConflict {
		t.Errorf("W5a-6 at intake.go:530: enrolling a whitespace-padded %q returned %d, want 409", w5aIdMaryName, rec.Code)
	}

	// (m5a-6): a name that matches NOBODY's composed Name but DOES match a
	// person's last_name must enrol. A duplicate check consulting the part
	// refuses here — and the officer is told a person is already on the
	// roster who is not.
	rec = postForm(t, w.srv, "/roster/intake", intakeForm2(w5aSharedLast, "Bob", w.companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("W5a-6 (m5a-6) at intake.go:530: enrolling %q returned %d, want 303. No person is CALLED %q — two are, separately, called %q and %q, and both merely carry last_name %q. A part that may diverge from the displayed name cannot be evidence about which person this is (D-48 derived ruling 1). Body: %s",
			composeName(w5aSharedLast, "Bob"), rec.Code, composeName(w5aSharedLast, "Bob"), w5aIdMaryName, w5aIdYukiName, w5aSharedLast, rec.Body.String())
	}
}

// =======================================================================
// SITE B — gate.go:839, resolveTypedPerson
// =======================================================================

func TestW5aResolveTypedPersonComparesComposedNameOnly(t *testing.T) {
	w := w5aBuildIdentityWorld(t)
	persons, err := domain.Persons(w.srv.Store)
	if err != nil {
		t.Fatal(err)
	}

	// (m5a-6b): case-differing input still resolves to the existing person.
	slot, err := resolveTypedPerson(w5aIdMaryUpper, persons)
	if err != nil {
		t.Fatalf("W5a-6 at gate.go:839: resolveTypedPerson(%q) errored: %v", w5aIdMaryUpper, err)
	}
	if slot.ExistingID != w.maryID {
		t.Errorf("W5a-6 (m5a-6b) at gate.go:839: resolveTypedPerson(%q) gave %+v, want ExistingID=%s. Matching is exact after trimming and CASE-INSENSITIVE so a name typed in a hurry resolves to the person rather than creating a near-duplicate; a mutant keeping TrimSpace and dropping EqualFold returns NewName here", w5aIdMaryUpper, slot, w.maryID)
	}

	// TrimSpace.
	if slot, err := resolveTypedPerson("  "+w5aIdMaryName+"  ", persons); err != nil || slot.ExistingID != w.maryID {
		t.Errorf("W5a-6 at gate.go:839: a whitespace-padded name gave %+v (err=%v), want ExistingID=%s", slot, err, w.maryID)
	}

	// (m5a-6): the shared last_name resolves to NOBODY — it is not anybody's
	// Name. A resolver consulting the part returns two hits and refuses,
	// naming a collision that does not exist.
	slot, err = resolveTypedPerson(w5aSharedLast, persons)
	if err != nil {
		t.Errorf("W5a-6 (m5a-6) at gate.go:839: resolveTypedPerson(%q) REFUSED (%v). Nobody is called %q; two people merely carry it as last_name, and the resolver compares composed Name only", w5aSharedLast, err, w5aSharedLast)
	} else if slot.NewName != w5aSharedLast || slot.ExistingID != "" {
		t.Errorf("W5a-6 (m5a-6) at gate.go:839: resolveTypedPerson(%q) gave %+v, want NewName=%q with no ExistingID", w5aSharedLast, slot, w5aSharedLast)
	}

	// And the exact composed Name resolves to exactly ONE person, not to
	// both people sharing the last_name.
	slot, err = resolveTypedPerson(w5aIdMaryName, persons)
	if err != nil {
		t.Errorf("W5a-6 (m5a-6) at gate.go:839: resolveTypedPerson(%q) REFUSED (%v) — only one person is called that; a resolver consulting last_name sees two and cannot say which is here", w5aIdMaryName, err)
	} else if slot.ExistingID != w.maryID {
		t.Errorf("W5a-6 at gate.go:839: resolveTypedPerson(%q) gave %+v, want ExistingID=%s", w5aIdMaryName, slot, w.maryID)
	}
}

// =======================================================================
// SITES C and D — gate.go:908 and gate.go:924, inside rowPersonSlot
// =======================================================================

// TestW5aRowPersonSlotComparesComposedNameOnly covers BOTH remaining person
// sites. :908 is the deliberate off-roster add; :924 is the picked-id
// honouring rule. Each has its own discriminator, because a mutant at one
// must not be masked by the other's correct behaviour.
func TestW5aRowPersonSlotComparesComposedNameOnly(t *testing.T) {
	w := w5aBuildIdentityWorld(t)
	st := w.srv.Store
	lookup := func(id string) (*domain.Person, error) { return domain.PersonByID(st, id) }
	all := func() ([]domain.Person, error) { return domain.Persons(st) }

	// --- SITE C, gate.go:908. The operator was shown the "not on the roster
	// — this will add them" state and left the name as it was. The row's
	// Value differs from Typed only in CASE.
	//
	// The discriminator: a person named "Ferguson, Mary" IS on the roster, so
	// with EqualFold the branch fires and returns a NEW-person slot, while
	// without it the call falls through to resolveTypedPerson and returns
	// that existing person's id. The two answers are different people.
	// ⟦[[D-84]], 2026-09-05⟧ the row's company/trade escape boxes are read
	// only when their PICKER says "not listed" (gate.go newPersonSlot —
	// intake's rule). The row posts that pick; the assertion below — the
	// off-roster add carries the row's typed company and trade — is
	// unchanged. ⟦was: Company/Trade alone, with no picker fields⟧
	slot, err := rowPersonSlot(passengerRow{
		Index: 0, Kind: "new", Value: w5aIdMaryUpper, Typed: w5aIdMaryName,
		CompanyID: newRef, Company: "Kakivik Asset Management",
		TradeID: newRef, Trade: "Inspection",
	}, lookup, all)
	if err != nil {
		t.Fatalf("W5a-6 at gate.go:908: rowPersonSlot errored: %v", err)
	}
	if slot.NewName != w5aIdMaryName || slot.ExistingID != "" {
		t.Errorf("W5a-6 (m5a-6b) at gate.go:908: the deliberate off-roster add gave %+v, want NewName=%q with no ExistingID. This site compares the SHOWN person name against the typed one with EqualFold(TrimSpace(…)); dropping EqualFold silently turns an add into a check-in of a different record", slot, w5aIdMaryName)
	}
	if slot.NewCompany != "Kakivik Asset Management" || slot.NewTrade != "Inspection" {
		t.Errorf("W5a-6 at gate.go:908: the off-roster add dropped the row's company/workgroup: %+v", slot)
	}

	// --- SITE D, gate.go:924. The hidden id is honoured only while it still
	// names a person whose roster name agrees with what is on the row.
	//
	// The discriminator: TWO people share the composed Name, so the picked id
	// is the ONLY thing that can say which is here. With EqualFold the id is
	// honoured; without it the call falls through to resolveTypedPerson,
	// which finds two and REFUSES — the duplicate display name stops being
	// resolvable at all, which is the exact case the site's own comment says
	// it exists for.
	srv2, st2 := w5aServer(t, "site-a", "")
	twinA := w5aPerson(t, st2, "site-a", w5aIdMaryName, w5aSharedLast, nil, nil)
	twinB := w5aPerson(t, st2, "site-a", w5aIdMaryName, "Nakamura", nil, nil)
	_ = srv2
	lookup2 := func(id string) (*domain.Person, error) { return domain.PersonByID(st2, id) }
	all2 := func() ([]domain.Person, error) { return domain.Persons(st2) }

	// Rule 6 — the arrangement: exactly two people carry this Name, and they
	// carry DIFFERENT last_names, so neither the Name nor the part can pick
	// between them and only the id can.
	twins, err := all2()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range twins {
		if p.Name == w5aIdMaryName {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("fixture defect: %d people are called %q, want exactly 2 — without the collision, gate.go:924's fall-through would resolve cleanly and the mutant would survive", n, w5aIdMaryName)
	}
	if twinA.PersonID == twinB.PersonID {
		t.Fatal("fixture defect: the two twins are one person")
	}

	slot, err = rowPersonSlot(passengerRow{
		Index: 1, Kind: "existing", Value: twinA.PersonID, Typed: w5aIdMaryUpper,
	}, lookup2, all2)
	if err != nil {
		t.Errorf("W5a-6 (m5a-6b) at gate.go:924: rowPersonSlot REFUSED a picked id whose person's Name matches the typed text in a different case (%v). The compare is EqualFold(TrimSpace(…)) on Name; dropping EqualFold makes the picked id fall through to the duplicate-name refusal, and a duplicate display name stops being resolvable at all", err)
	} else if slot.ExistingID != twinA.PersonID {
		t.Errorf("W5a-6 (m5a-6b) at gate.go:924: rowPersonSlot gave %+v, want ExistingID=%s — the operator picked ONE of two identically-named people and their id, not the name, is what gets checked in", slot, twinA.PersonID)
	}

	// (m5a-6) at this site: the picked id must be DISHONOURED when the
	// visible text no longer names that person — and "no longer names" is
	// decided on Name, never on the part. twinB's last_name is "Nakamura";
	// typing a text that matches neither twin's Name must not resolve to
	// either id.
	slot, err = rowPersonSlot(passengerRow{
		Index: 2, Kind: "existing", Value: twinB.PersonID, Typed: w5aSharedLast,
	}, lookup2, all2)
	if err != nil {
		t.Errorf("W5a-6 (m5a-6) at gate.go:924: typing %q against a picked id REFUSED (%v); nobody is called %q, so it must fall through to a new-person slot", w5aSharedLast, err, w5aSharedLast)
	} else if slot.ExistingID != "" || slot.NewName != w5aSharedLast {
		t.Errorf("W5a-6 (m5a-6) at gate.go:924: typing %q against picked id %s gave %+v, want a NEW-person slot. twinA carries last_name %q; honouring the id because the PART matches is the visible text losing its authority over identity", w5aSharedLast, twinB.PersonID, slot, w5aSharedLast)
	}

	// Guard against a lazy read of the two assertions above: the typed text
	// really is not either twin's Name.
	if strings.EqualFold(strings.TrimSpace(w5aSharedLast), w5aIdMaryName) {
		t.Fatal("fixture defect: the shared last name equals the composed Name")
	}
}
