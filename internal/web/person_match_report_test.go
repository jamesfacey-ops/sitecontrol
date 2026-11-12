// person_match_report_test.go — the census face of the asking rule, pinned
// to the rule itself: every row of officerVariants (person_match_test.go)
// must Read exactly as it offers, so cmd/dupreport can never describe a rule
// the gate does not run; and the attribution (comma swap or not, which key
// signature) is pinned on the pairs whose class is not obvious — both sides
// in intake form, a suffix segment on the gate side, the price row.
package web

import "testing"

// TestNameMatchReportReadsExactlyWhatTheAskingRuleOffers is the anti-drift
// pin: the report's Reads column IS the table's offer column, in both
// directions, and CommaSwap and Shape are symmetric too.
func TestNameMatchReportReadsExactlyWhatTheAskingRuleOffers(t *testing.T) {
	if len(officerVariants) < 22 {
		t.Fatalf("the table holds %d rows; the reviewer's 22 shapes are the floor", len(officerVariants))
	}
	for _, v := range officerVariants {
		r := ReportNameMatch(v.roster, v.typed)
		if r.Reads != v.offer {
			t.Errorf("ReportNameMatch(%q, %q).Reads = %v, the rule offers %v — %s", v.roster, v.typed, r.Reads, v.offer, v.why)
		}
		if back := ReportNameMatch(v.typed, v.roster); back != r {
			t.Errorf("ReportNameMatch(%q, %q) = %+v but reversed = %+v — the census must be symmetric like the rule", v.roster, v.typed, r, back)
		}
		if !r.Reads && (r.Exact || r.CommaSwap || r.Shape != "") {
			t.Errorf("ReportNameMatch(%q, %q) does not Read yet carries %+v", v.roster, v.typed, r)
		}
		if r.Exact && (r.CommaSwap || r.Shape != "") {
			t.Errorf("ReportNameMatch(%q, %q) is Exact yet carries a loose attribution %+v", v.roster, v.typed, r)
		}
		if r.Reads && !r.Exact && r.Shape == "" {
			t.Errorf("ReportNameMatch(%q, %q) is a loose match with no Shape", v.roster, v.typed)
		}
	}
}

// TestNameMatchReportAttributesTheCommaSwapOnlyToTwoConventionPairs pins the
// class boundary: the swap must have been NECESSARY and the two sides must be
// in DIFFERENT conventions. The rows that could be mis-attributed either
// way are the point of this table.
func TestNameMatchReportAttributesTheCommaSwapOnlyToTwoConventionPairs(t *testing.T) {
	cases := []struct {
		a, b      string
		reads     bool
		exact     bool
		commaSwap bool
		shape     NameMatchShape
		why       string
	}{
		// --- the class: one side "Last, First", the other "First Last" ----
		{"Ferguson, Mary", "Mary Ferguson", true, false, true, ShapeSameLetters, "the reviewer's end-to-end minted duplicate"},
		{"Ferguson,Mary", "Mary Ferguson", true, false, true, ShapeSameLetters, "comma without a space"},
		{"Ferguson, Mary J", "Mary Ferguson", true, false, true, ShapeOneLetterMiddle, "swap + a middle initial: the swap was still necessary"},
		{"Ferguson, Mary Jane", "Mary Ferguson", true, false, true, ShapeMiddleName, "swap + a middle name"},
		{"McDonald, Pat", "Pat Mc Donald", true, false, true, ShapeSpacing, "swap + spacing"},
		{"Passenger, Pat, Jr", "Pat Passenger", true, false, true, ShapeSameLetters, "intake form with a suffix segment"},
		{"Pat Passenger, Sr.", "Passenger, Pat", true, false, true, ShapeSameLetters, "a suffix segment on the GATE side puts a comma on both sides — still two conventions, detected by the swap's effect, not by counting commas"},
		{"Scott, James", "James Scott", true, false, true, ShapeSameLetters, "the comma is the signal even when both words are first names"},
		// --- NOT the class: joined, but the swap did not do the joining -----
		{"Ferguson, Mary", "Ferguson,Mary", true, false, false, ShapeSameLetters, "both in intake form"},
		{"O Brien, Pat", "Brien, Pat", true, false, false, ShapeOneLetterMiddle, "both in intake form: the swap applied to both and joined nothing; the initial skip did"},
		{"O'Brien, Pat", "O Brien, Pat", true, false, false, ShapeParticleKept, "both in intake form, the particle kept"},
		{"Pat O Brien", "Pat Brien", true, false, false, ShapeOneLetterMiddle, "THE PRICE ROW (person_match_test.go): no comma anywhere"},
		{"Mary J. Ferguson", "Mary Ferguson", true, false, false, ShapeOneLetterMiddle, "the real middle-initial legacy case — the SAME shape as the price row, by design of the rule"},
		{"Pat Mc Donald", "Pat McDonald", true, false, false, ShapeSpacing, "the legacy spacing case"},
		{"Pat J Mc Donald", "Pat McDonald", true, false, false, ShapeInitialAndSpacing, "initial + spacing"},
		{"Pat Passenger", "Pat Passenger Jr", true, false, false, ShapeSameLetters, "suffix as a word"},
		{"Pat Passenger", "Pat Passenger, Sr.", true, false, false, ShapeSameLetters, "suffix as a comma segment is not a swap"},
		{"Pat O'Brien", "Pat O Brien", true, false, false, ShapeParticleKept, "the kept-word catch"},
		// --- exact: the strict key's own territory ---------------------------
		{"Pat Passenger", "PAT  PASSENGER", true, true, false, "", "case and inner whitespace"},
		{"Ferguson, Mary", "ferguson, mary", true, true, false, "", "case, both in intake form"},
		// --- not joined at all ----------------------------------------------
		{"James Scott", "Scott James", false, false, false, "", "no comma marks the order"},
		{"Ferguson, Mary", "Ferguson Mary", false, false, false, "", "no comma on the typed side"},
		{"Dana Driver", "Pat Passenger", false, false, false, "", "two people"},
	}
	for _, c := range cases {
		got := ReportNameMatch(c.a, c.b)
		want := NameMatchReport{Reads: c.reads, Exact: c.exact, CommaSwap: c.commaSwap, Shape: c.shape}
		if got != want {
			t.Errorf("ReportNameMatch(%q, %q) = %+v, want %+v — %s", c.a, c.b, got, want, c.why)
		}
	}
}

// TestCommaSwappedIsDetectedByTheFoldsEffectNotByCountingCommas pins the
// one-side test the CommaSwap class rests on: a suffix segment and a
// two-comma name carry commas the rule does not swap on, and a bare comma
// form is swapped whether or not a space follows the comma.
func TestCommaSwappedIsDetectedByTheFoldsEffectNotByCountingCommas(t *testing.T) {
	for _, s := range []string{"Ferguson, Mary", "Ferguson,Mary", "Passenger, Pat, Jr", "O Brien, Pat", "Scott, James"} {
		if !commaSwapped(s) {
			t.Errorf("commaSwapped(%q) = false, want true: fold 1 reads this as Last, First", s)
		}
	}
	for _, s := range []string{"Mary Ferguson", "Pat Passenger, Sr.", "Ferguson, Mary, Jane", "Pat Passenger", "", ",", ", Jr"} {
		if commaSwapped(s) {
			t.Errorf("commaSwapped(%q) = true, want false: fold 1 does not swap this spelling", s)
		}
	}
}
