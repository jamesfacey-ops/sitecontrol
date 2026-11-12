// w5fix_refdata_typed_test.go — the TYPED half of fork/w5's reference-data
// fixes (see w5fix_refdata_test.go): assertions that name symbols the fix
// introduced — CompanyRenameRefusal, Person.IsRetired, SameTradeName /
// SameCompanyName — and so cannot compile on the unfixed tree. Kept apart
// from the probe file so that file alone can be run against the pre-fix
// tree to show the fail-before.
package domain

import (
	"errors"
	"testing"
)

// TestR145TheRenameRefusalCarriesTheHolderAsData: the refusal names the
// company already holding the name BY TYPE (Existing is the Kestrel row, by
// identity), is ALSO a *CompanyAmendRefusal through Unwrap (the class the web
// partition matches), and its sentence carries the on-list spelling.
func TestR145TheRenameRefusalCarriesTheHolderAsData(t *testing.T) {
	st := newDomainTestStore(t)
	kestrel := w5fixCompany(t, st, "site-a", "Kestrel Survey Group")
	glacier := w5fixCompany(t, st, "site-a", "Glacier Line Electric")
	name := "kestrel survey group"
	_, err := AmendCompany(st, "site-a", 1, CompanyAmendment{CompanyID: glacier.CompanyID, Name: &name}, 0, "default")
	var refusal *CompanyRenameRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a *CompanyRenameRefusal", err)
	}
	if refusal.Existing.CompanyID != kestrel.CompanyID {
		t.Errorf("Existing.CompanyID = %q, want the holder %q", refusal.Existing.CompanyID, kestrel.CompanyID)
	}
	if refusal.Renamed.CompanyID != glacier.CompanyID || refusal.Typed != name {
		t.Errorf("Renamed/Typed = %q/%q, want %q/%q", refusal.Renamed.CompanyID, refusal.Typed, glacier.CompanyID, name)
	}
	var class *CompanyAmendRefusal
	if !errors.As(err, &class) {
		t.Error("the rename refusal does not unwrap to *CompanyAmendRefusal — the web layer would route it to the internal-fault arm")
	} else if class.Reason != refusal.Reason() {
		t.Errorf("unwrapped Reason %q differs from Reason() %q", class.Reason, refusal.Reason())
	}
}

// TestR118IsRetiredReadsTheValueDomainAndNothingElse pins the ONE read-side
// rule every selection surface consults: "true"/"yes"/"1" (any case, padded)
// read as retired; nil, "false" and anything else do not.
func TestR118IsRetiredReadsTheValueDomainAndNothingElse(t *testing.T) {
	for v, want := range map[string]bool{"true": true, "TRUE": true, " yes ": true, "1": true, "false": false, "0": false, "": false, "retired": false} {
		val := v
		if got := (Person{Retired: &val}).IsRetired(); got != want {
			t.Errorf("IsRetired(%q) = %v, want %v", v, got, want)
		}
	}
	if (Person{}).IsRetired() {
		t.Error("IsRetired() with no retired fact = true")
	}
}

// TestR087TheExportedFoldsAgreeWithTheDomainDoors: the web layer's intake
// pre-checks read the SAME fold the domain refuses by, so a pair the
// pre-check lets through can no longer be refused one call later as a 500.
func TestR087TheExportedFoldsAgreeWithTheDomainDoors(t *testing.T) {
	for _, pair := range [][2]string{{"Cook Helper", "Cook  Helper"}, {"cook helper", " COOK HELPER "}, {"Cook\tHelper", "Cook Helper"}} {
		if !SameTradeName(pair[0], pair[1]) || !sameTradeName(pair[0], pair[1]) {
			t.Errorf("SameTradeName(%q, %q) = false", pair[0], pair[1])
		}
	}
	if SameTradeName("Cook.", "Cook") {
		t.Error("SameTradeName folds punctuation — that widening needs a ruling")
	}
	if !SameCompanyName(" kestrel survey group", "Kestrel Survey Group ") || SameCompanyName("Kestrel", "Kestrel Ltd") {
		t.Error("SameCompanyName does not match checkin.go's sameCompanyName")
	}
}
