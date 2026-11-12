// witness_u8_picker_test.go — witness phase, u8-post19 assignment
// (reports/witness/u8-post19.md), secondary to this phase's unsolicited-
// arrival work (witness_u8_post19_test.go). Kills two post-19eafc4 census
// survivors (reports/witness/u2-census-rerun.md, post19_unreached.tsv):
//
//	picker.go:410-415  refSameNameRows  `if ids[j] == "" {`  (Value fallback)
//	picker.go:444-446  personRefs       `} else {`           (whole-id, no ellipsis)
//
// Both functions are called directly, unexported, in this same package —
// picker_same_name_test.go's existing coverage goes through the full
// onSiteRow -> renderPicker pipeline, where every fixture row carries a
// real 20-character person id, so neither an empty PersonID nor an id
// short enough to skip truncation is ever produced. That is exactly what
// leaves these two arms unreached: they are real defensive/precision code,
// just never fed the input that exercises them.
package web

import "testing"

// TestWitnessU8RefSameNameRowsFallsBackToValueWhenPersonIDIsEmpty kills
// picker.go:410-415. refSameNameRows' own comment says no row in this
// package is built without a person id today, but the fallback exists
// because a tag made from "" is no tag at all — this pins what it does
// when fed exactly that input, directly, rather than trusting the comment.
func TestWitnessU8RefSameNameRowsFallsBackToValueWhenPersonIDIsEmpty(t *testing.T) {
	rows := []pickerRow{
		{Value: "typed-value-1", PersonID: ""},
		{Value: "typed-value-2", PersonID: "site-a-person-abc123"},
	}
	refSameNameRows(rows, []int{0, 1})

	const want0 = "person id …ue-1"
	if rows[0].Ref != want0 {
		t.Fatalf("row with empty PersonID: Ref = %q, want %q (tail of Value %q) — an empty id must fall "+
			"back to the submitted Value, not render a blank tag", rows[0].Ref, want0, rows[0].Value)
	}
	const want1 = "person id …c123"
	if rows[1].Ref != want1 {
		t.Fatalf("row with a real PersonID: Ref = %q, want %q", rows[1].Ref, want1)
	}
}

// TestWitnessU8PersonRefsRendersAWholeShortIDWithNoEllipsis kills
// picker.go:444-446. The ellipsis is a promise, per personRefs' own doc
// comment ("prefixed with an ellipsis when it is NOT the whole id") — an id
// no longer than the current width is shown whole, and prefixing it anyway
// would tell an officer a full id has been truncated when it has not.
func TestWitnessU8PersonRefsRendersAWholeShortIDWithNoEllipsis(t *testing.T) {
	got := personRefs([]string{"abcd", "site-a-person-abc123"})
	if len(got) != 2 {
		t.Fatalf("personRefs returned %d tails, want 2: %v", len(got), got)
	}
	if got[0] != "abcd" {
		t.Fatalf("tail for the 4-character id = %q, want %q verbatim — it must never be ellipsis-prefixed, "+
			"since it is not truncated", got[0], "abcd")
	}
	if got[1] != "…c123" {
		t.Fatalf("tail for the long id = %q, want %q", got[1], "…c123")
	}
}
