// w5fix_refdata_test.go — fork/w5 (2026-10-03): the reference-data edge
// defects L06 measured twice (lanes/L06.md, lanes-r2/L06.md), each pinned
// through the REAL authoring edges with only pre-existing API, so every test
// here was run against the unfixed tree first and FAILED there:
//
//	R-113 / M-19  AmendPerson accepted a company_id naming no company row.
//	R-145 / M-18  AmendCompany renamed a company onto another's exact name.
//	R-118 / M-8   `retired` was recorded and read by nothing (D-21.5 unbuilt).
//	R-087         "Cook  Helper" and "Cook Helper" were two trades.
//
// The TYPED half of each (CompanyRenameRefusal, Person.IsRetired, the
// exported folds) lives in w5fix_refdata_typed_test.go, which cannot compile
// on the unfixed tree and is therefore kept out of the fail-before run.
package domain

import (
	"errors"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w5fixWatermark is the site-a/epoch-1 authoring watermark: a refusal never
// calls Author, so it must not move across a refused amendment.
func w5fixWatermark(t *testing.T, st *store.Store) int64 {
	t.Helper()
	wm, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

func w5fixPerson(t *testing.T, st *store.Store, name string, companyID *string) Person {
	t.Helper()
	p, err := CreatePerson(st, "site-a", 1, name, companyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func w5fixCompany(t *testing.T, st *store.Store, siteID, name string) Company {
	t.Helper()
	c, err := CreateCompany(st, siteID, 1, name, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func w5fixComposedPerson(t *testing.T, st *store.Store, id string) Person {
	t.Helper()
	p, err := PersonByID(st, id)
	if err != nil || p == nil {
		t.Fatalf("PersonByID(%s) = %v, %v", id, p, err)
	}
	return *p
}

func w5fixCompanyNamed(t *testing.T, st *store.Store, id string) string {
	t.Helper()
	c, err := CompanyByID(st, id)
	if err != nil || c == nil {
		t.Fatalf("CompanyByID(%s) = %v, %v", id, c, err)
	}
	return c.Name
}

// --- R-113: unknown company_id ------------------------------------------------

// TestR113AmendPersonRefusesAnUnknownCompanyIDAndAuthorsNothing: the same
// edge refuses an unknown trade_id and ACCEPTED an unknown company_id (L06
// R-113: company_id "bogus" → "Recorded. Effective: company_id.", roster
// cell "bogus"). Refused now, pre-append; a known id and a MERGED-AWAY id
// (whose row survives) are both still accepted — the two positive controls
// that keep this from being an always-refuse arm.
func TestR113AmendPersonRefusesAnUnknownCompanyIDAndAuthorsNothing(t *testing.T) {
	st := newDomainTestStore(t)
	tundra := w5fixCompany(t, st, "site-a", "Tundra Services")
	p := w5fixPerson(t, st, "Dunby, Logan", &tundra.CompanyID)
	before := w5fixWatermark(t, st)

	bogus := "bogus"
	_, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, CompanyID: &bogus}, 0, "default")
	if err == nil {
		t.Fatal("AmendPerson(company_id = \"bogus\") = nil — an id naming no company row was ACCEPTED (R-113)")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "company") {
		t.Errorf("the refusal does not name the id and the register: %v", err)
	}
	if got := w5fixWatermark(t, st); got != before {
		t.Errorf("watermark moved %d → %d across a refused amendment — the refusal is not pre-append", before, got)
	}
	if got := w5fixComposedPerson(t, st, p.PersonID); got.CompanyID == nil || *got.CompanyID != tundra.CompanyID {
		t.Errorf("composed company_id = %v, want the unchanged %s", got.CompanyID, tundra.CompanyID)
	}

	// Positive control 1: a KNOWN company id records.
	kestrel := w5fixCompany(t, st, "site-a", "Kestrel Survey Group")
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, CompanyID: &kestrel.CompanyID}, 0, "default"); err != nil {
		t.Fatalf("a known company id was refused: %v", err)
	}
	if got := w5fixComposedPerson(t, st, p.PersonID); got.CompanyID == nil || *got.CompanyID != kestrel.CompanyID {
		t.Errorf("composed company_id = %v, want %s", got.CompanyID, kestrel.CompanyID)
	}
	// Positive control 2: a MERGED-AWAY company id is still a `company` row
	// and is accepted — resolution happens at the read, as for trade ids.
	glacier := w5fixCompany(t, st, "site-a", "Glacier Line Electric")
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, SameAs: &kestrel.CompanyID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if c, _ := CompanyByID(st, glacier.CompanyID); c == nil || c.SameAs == nil {
		t.Fatal("precondition: Glacier is not composed as merged into Kestrel")
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, CompanyID: &glacier.CompanyID}, 0, "default"); err != nil {
		t.Errorf("a merged-away company id was refused: %v — the arm must ask \"is this a company row?\", never \"is it merged?\"", err)
	}
}

// --- R-145: company rename onto another company's name -------------------------

// TestR145CompanyRenameOntoAnotherCompanysNameIsRefusedAuthorsNothingAndNeverMerges
// is D-82's rule on the company edge. L06 R-145: Glacier Line Electric
// renamed to "Kestrel Survey Group" → "Recorded. Effective: name." → two
// identical company headings on the live muster. Refused now, by the
// fact-domain class web/company_amend.go matches (*CompanyAmendRefusal), the
// watermark unmoved, Glacier still named Glacier. The three positive
// controls are as load-bearing as the refusal: a company's OWN name in a
// different case still records (a case fix is not a duplicate of itself), a
// name FREED by an earlier rename is takeable, and the merge D-82 forbids
// folding into a rename is still available as its own same_as.
func TestR145CompanyRenameOntoAnotherCompanysNameIsRefusedAuthorsNothingAndNeverMerges(t *testing.T) {
	st := newDomainTestStore(t)
	kestrel := w5fixCompany(t, st, "site-a", "Kestrel Survey Group")
	glacier := w5fixCompany(t, st, "site-a", "Glacier Line Electric")
	before := w5fixWatermark(t, st)

	for _, typed := range []string{"Kestrel Survey Group", "kestrel survey group", "  Kestrel Survey Group "} {
		name := typed
		_, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, Name: &name}, 0, "default")
		if err == nil {
			t.Fatalf("AmendCompany(Glacier → %q) = nil with Kestrel listed — ACCEPTED (R-145); two companies now share one muster heading", typed)
		}
		var refusal *CompanyAmendRefusal
		if !errors.As(err, &refusal) {
			t.Errorf("rename onto %q: %v is not a *CompanyAmendRefusal — web/company_amend.go's partition would route it to the internal-fault arm", typed, err)
		} else if !strings.Contains(refusal.Reason, kestrel.CompanyID) {
			t.Errorf("the refusal does not name the company already holding the name (%s): %q", kestrel.CompanyID, refusal.Reason)
		}
	}
	if got := w5fixWatermark(t, st); got != before {
		t.Errorf("watermark moved %d → %d across refused renames — not pre-append", before, got)
	}
	if got := w5fixCompanyNamed(t, st, glacier.CompanyID); got != "Glacier Line Electric" {
		t.Errorf("Glacier is now named %q — the refusal did not hold", got)
	}
	if c, _ := CompanyByID(st, glacier.CompanyID); c != nil && c.SameAs != nil {
		t.Errorf("Glacier composed with same_as=%s — a refused rename MERGED (D-82: refuse, never merge)", *c.SameAs)
	}

	// Control: own name, case-corrected — not a collision with itself.
	own := "GLACIER LINE ELECTRIC"
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, Name: &own}, 0, "default"); err != nil {
		t.Errorf("a company's own name in a different case was refused: %v", err)
	}
	// Control: a name freed by a rename is takeable.
	freed := "Kestrel Survey Group Ltd"
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: kestrel.CompanyID, Name: &freed}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	take := "Kestrel Survey Group"
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, Name: &take}, 0, "default"); err != nil {
		t.Errorf("a name freed by a rename was refused: %v — the guard must compare against the COMPOSED list", err)
	}
	// Control: the merge is still its own amendment.
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, SameAs: &kestrel.CompanyID}, 0, "default"); err != nil {
		t.Errorf("same_as after the refused rename: %v — merging must remain available as the separate amendment", err)
	}
}

// --- R-118: retired is read, guarded and reversible ---------------------------

// TestR118RetiredAcceptsOnlyTrueAndUnRetiringIsTheClear pins the value
// domain the edge now guards: a value that is not "true" (or yes/1) is
// refused rather than recorded as an opaque string nobody reads; "true"
// records; Clear:["retired"] un-retires (D-21.5: reversible).
func TestR118RetiredAcceptsOnlyTrueAndUnRetiringIsTheClear(t *testing.T) {
	st := newDomainTestStore(t)
	p := w5fixPerson(t, st, "Dunby, Logan", nil)
	before := w5fixWatermark(t, st)
	for _, bad := range []string{"false", "maybe", "retired", "0"} {
		v := bad
		if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Retired: &v}, 0, "default"); err == nil {
			t.Errorf("Retired=%q was ACCEPTED — the fact would be recorded as a string no reader understands", bad)
		}
	}
	if got := w5fixWatermark(t, st); got != before {
		t.Errorf("watermark moved %d → %d across refused values", before, got)
	}
	for _, ok := range []string{"true", "TRUE", "yes", "1"} {
		v := ok
		if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Retired: &v}, 0, "default"); err != nil {
			t.Errorf("Retired=%q was refused: %v", ok, err)
		}
		if got := w5fixComposedPerson(t, st, p.PersonID); got.Retired == nil || *got.Retired != "true" {
			t.Errorf("after Retired=%q the composed value is %v, want the canonical \"true\"", ok, got.Retired)
		}
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Clear: []string{"retired"}}, 0, "default"); err != nil {
		t.Fatalf("Clear retired (the un-retire) was refused: %v", err)
	}
	if got := w5fixComposedPerson(t, st, p.PersonID); got.Retired != nil {
		t.Errorf("after Clear retired the composed value is %q, want absent — retirement must be reversible", *got.Retired)
	}
}

// --- R-087: inner whitespace is one trade --------------------------------------

// TestR087TradeNamesDifferingOnlyByInnerWhitespaceAreOneTrade: "Cook Helper"
// listed, "Cook  Helper" (two spaces) was accepted as a second trade (L06
// R-087) — a browser collapses the run, so the muster printed two headings
// that read identically. Both doors (create and rename) now fold inner
// whitespace; punctuation is deliberately still distinct ("Cook." is not
// "Cook"), pinned as the boundary D-76's "nothing else" leaves.
func TestR087TradeNamesDifferingOnlyByInnerWhitespaceAreOneTrade(t *testing.T) {
	st := newDomainTestStore(t)
	helper, err := CreateTrade(st, "site-a", 1, "Cook Helper", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	cook, err := CreateTrade(st, "site-a", 1, "Cook", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := w5fixWatermark(t, st)
	for _, variant := range []string{"Cook  Helper", "Cook\tHelper", " Cook   Helper "} {
		_, err := CreateTrade(st, "site-a", 1, variant, 0, "default")
		var refusal *TradeCreateRefusal
		if !errors.As(err, &refusal) {
			t.Errorf("CreateTrade(%q) with %q listed = %v, want a *TradeCreateRefusal (R-087)", variant, helper.Name, err)
		}
		name := variant
		_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: cook.TradeID, Name: &name}, 0, "default")
		var rename *TradeRenameRefusal
		if !errors.As(err, &rename) {
			t.Errorf("AmendTrade(Cook → %q) with %q listed = %v, want a *TradeRenameRefusal (R-087 on the D-82 door)", variant, helper.Name, err)
		}
	}
	if got := w5fixWatermark(t, st); got != before {
		t.Errorf("watermark moved %d → %d across refused creates/renames", before, got)
	}
	// The boundary: punctuation stays distinct, so this is still a separate trade.
	if _, err := CreateTrade(st, "site-a", 1, "Cook.", 0, "default"); err != nil {
		t.Errorf("CreateTrade(\"Cook.\") with \"Cook\" listed = %v — punctuation is NOT folded (D-76 \"nothing else\"); widening past whitespace needs a ruling", err)
	}
}
