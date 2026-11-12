// person_match_test.go — the rule in person_match.go, proved in BOTH
// directions on one table of officer variants:
//
//   - every fold it makes, pinned row by row (the three real legacy cases and
//     the comma swap this system manufactures among them);
//   - every shape it REFUSES to fold, pinned so a later widening is a rule
//     change made on purpose — the rows are pairs a sloppier rule (edit
//     distance, bag of words, surname-only, initial-for-name) would join;
//   - and, for EVERY row, that the strict key (resolveTypedPerson) adopts
//     exactly the spellings it always did. The loose key asks; nothing here
//     widened what resolves.
//
// TestNameFoldStepsInIsolation then pins each fold step on its own, so a
// row that stops offering can be traced to the one step that changed.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// nameVariant is one officer spelling against one roster name.
type nameVariant struct {
	roster, typed string
	offer         bool   // does personMatchCandidates offer the roster row?
	why           string // the fold that catches it, or the reason it must not
}

// officerVariants — the first 22 rows are the shapes the reviewer measured
// against 72d6111 (reports/witness/REVIEW-PERSON-DUP.md: only the FIRST row
// offered there), each shape given in the direction(s) the two doors
// manufacture; the rest are this change's own catches and its pinned
// refusals.
var officerVariants = []nameVariant{
	// --- the reviewer's 22 shapes ------------------------------------------
	{"Pat Passenger", "PAT  PASSENGER", true, "case + doubled inner space: sameName — the ONE spelling 72d6111 offered on"},
	{"Pat Passenger", "pat passenger", true, "case only: sameName, and the strict key adopts it too"},
	{"Pat Passenger", "Passenger, Pat", true, "comma swap, intake spelling typed at the gate"},
	{"Passenger, Pat", "Pat Passenger", true, "comma swap, gate spelling arriving at intake"},
	{"Ferguson, Mary", "Mary Ferguson", true, "comma swap — the reviewer's end-to-end minted duplicate"},
	{"Ferguson, Mary", "Ferguson Mary", false, "REJECTED: no comma marks the order (see James Scott / Scott James below)"},
	{"Ferguson, Mary", "Ferguson,Mary", true, "comma without a space: the split does not need one"},
	{"Ferguson, Mary", "Mary J. Ferguson", true, "middle initial on the typed side: first-and-last key"},
	{"Ferguson, Mary J", "Mary Ferguson", true, "middle initial on the roster side: first-and-last key — the legacy case"},
	{"Ferguson, Mary J.", "Ferguson, Mary", true, "middle initial, both in intake form"},
	{"Pat Mc Donald", "Pat McDonald", true, "spacing inside the surname: run-together key — legacy case ×2"},
	{"McDonald, Pat", "Pat Mc Donald", true, "spacing + comma swap together"},
	{"O'Brien, Pat", "OBrien, Pat", true, "apostrophe dropped"},
	{"Pat OBrien", "Pat O'Brien", true, "apostrophe dropped, gate form"},
	{"Pat Vanderberg", "Pat Van der Berg", true, "particles run together"},
	{"Van der Berg, Pat", "Pat Vanderberg", true, "particles + comma swap"},
	{"Pat Passenger", "Pat Passenger Jr", true, "generational suffix as a trailing word"},
	{"Passenger, Pat Jr", "Pat Passenger", true, "suffix inside the first segment of the intake form"},
	{"Pat Passenger", "Pat Passengr", false, "REJECTED: a one-letter typo is edit distance"},
	{"Pat Passenger", "Pat Pasenger", false, "REJECTED: a one-letter typo is edit distance"},
	{"Ferguson, Mary", "Ferguson, Marie", false, "a real spelling difference"},
	{"Dana Driver", "Pat Passenger", false, "two different people"},
	// --- this change's own catches -----------------------------------------
	{"Ferguson, Mary Jane", "Mary Ferguson", true, "a full middle name on the ID card, the spoken name at the gate"},
	{"Mary Ann Smith", "Maryann Smith", true, "a first name split or joined: run-together key"},
	{"Pat J Mc Donald", "Pat McDonald", true, "initial + split surname: run-together key skips the initial"},
	{"Pat O'Brien", "Pat O Brien", true, "a lone O before the surname: the kept-word run-together key — MEASURED a MISS at 9217dff, where the skipping form alone dropped the O and read Brien as the surname"},
	{"O'Brien, Pat", "O Brien, Pat", true, "the same pair in intake form"},
	{"Pat D'Angelo", "Pat D Angelo", true, "the same shape with a D"},
	{"Pat O Brien", "Pat Brien", true, "PRICE, stated: the first-and-last key reads the lone O as an initial, so the space form is offered for the bare surname too; the O is visible in the offered name and it only asks"},
	{"José García", "Jose Garcia", false, "REJECTED (see person_match.go): accents are not folded — the decomposition would be internal/web's first third-party import, and the W6 witness cannot type-check it"},
	{"Smith-Jones, Pat", "Pat Smith Jones", true, "a hyphen reads as a space"},
	{"Pat Passenger III", "Pat Passenger", true, "suffix III"},
	{"Pat Passenger", "Pat Passenger, Sr.", true, "suffix as its own comma segment"},
	{"Passenger, Pat, Jr", "Pat Passenger", true, "suffix segment after the intake form's comma"},
	{"Pat Passenger", " pat passenger ", true, "trim + case: sameName, and the strict key adopts it too"},
	{"Scott, James", "James Scott", true, "comma swap where both words are first names — the comma IS the signal"},
	// --- pinned refusals: pairs a sloppier rule would join -----------------
	{"James Scott", "Scott James", false, "REJECTED: bag-of-words would join two real people"},
	{"Dan Brown", "Don Brown", false, "REJECTED: edit distance would join"},
	{"Jon Snow", "John Snow", false, "REJECTED: phonetics would join"},
	{"John Smith", "J Smith", false, "REJECTED: an initial does not stand for a first name"},
	{"Pat Mc Donald", "Sam Mc Donald", false, "a surname-only rule would join"},
	{"Pat Passenger", "Passenger", false, "surname alone"},
	{"Pat Passenger", "Pat", false, "first name alone"},
	{"Pat Sr", "Pat", false, "a two-word name keeps its second word"},
	{"Mary Ferguson", "Maryann Ferguson", false, "STATED MISS: a joined first name is a different first word"},
	{"Pat Passenger", "Pat Passenger, Kakivik", false, "STATED MISS: a company typed into the name box"},
	{"Ferguson, Mary", "Ferguson, Mary, Jane", false, "STATED MISS: two commas mark nothing this rule understands"},
}

func rosterOf(names ...string) []domain.Person {
	var out []domain.Person
	for i, n := range names {
		out = append(out, domain.Person{PersonID: "p" + string(rune('a'+i)), Name: n})
	}
	return out
}

// TestPersonMatchOfficerVariants is the table, both columns: the offer the
// loose key makes, and the adoption the strict key does NOT.
func TestPersonMatchOfficerVariants(t *testing.T) {
	if len(officerVariants) < 22 {
		t.Fatalf("the table holds %d rows; the reviewer's 22 shapes are the floor", len(officerVariants))
	}
	offers, refusals := 0, 0
	for _, v := range officerVariants {
		roster := rosterOf(v.roster)
		got := personMatchCandidates(roster, v.typed, "")
		if v.offer && len(got) != 1 {
			t.Errorf("%q typed against roster %q: %d candidate(s), want 1 — %s", v.typed, v.roster, len(got), v.why)
		}
		if !v.offer && len(got) != 0 {
			t.Errorf("%q typed against roster %q: %d candidate(s), want 0 — %s", v.typed, v.roster, len(got), v.why)
		}
		// Symmetric by construction: the roster spelling typed against the
		// typed spelling must answer the same, or one door would catch what
		// the other misses.
		if back := personMatchCandidates(rosterOf(v.typed), v.roster, ""); (len(back) == 1) != v.offer {
			t.Errorf("%q against %q answers %v but the reverse answers %v — the rule must be symmetric", v.typed, v.roster, v.offer, len(back) == 1)
		}
		// THE STRICT KEY IS UNCHANGED: it adopts exactly when TrimSpace +
		// EqualFold says so, offer or no offer. Every folded row below that
		// line is a NEW record until the officer picks.
		strictAdopts := strings.EqualFold(strings.TrimSpace(v.roster), strings.TrimSpace(v.typed))
		slot, err := resolveTypedPerson(v.typed, roster)
		if err != nil {
			t.Fatalf("resolveTypedPerson(%q): %v", v.typed, err)
		}
		if (slot.ExistingID != "") != strictAdopts {
			t.Errorf("resolveTypedPerson(%q) against %q: ExistingID=%q, want adoption=%v — the loose key must not widen what auto-adopts", v.typed, v.roster, slot.ExistingID, strictAdopts)
		}
		if !strictAdopts && slot.NewName == "" {
			t.Errorf("resolveTypedPerson(%q) produced neither a record nor a new name", v.typed)
		}
		if v.offer {
			offers++
		} else {
			refusals++
		}
	}
	t.Logf("%d rows: %d offer, %d refuse; the strict key adopted only the trim/case rows", len(officerVariants), offers, refusals)

	// A blank typed name matches nobody — not even a blank roster name — and
	// a blank roster name is offered to nobody.
	if got := personMatchCandidates(rosterOf(""), "   ", ""); len(got) != 0 {
		t.Errorf("a blank name produced %d candidate(s)", len(got))
	}
	if got := personMatchCandidates(rosterOf("", "  ", ","), "Pat Passenger", ""); len(got) != 0 {
		t.Errorf("blank roster rows were offered: %d", len(got))
	}
}

// TestPersonMatchOffersEveryLegacyCaseAndTheCommaSwap is the headline on its
// own, so the four cases the shipped rule missed cannot go quiet inside the
// table: a roster holding the three legacy shapes and the intake spelling,
// typed the other way, offers all four and nobody else.
func TestPersonMatchOffersEveryLegacyCaseAndTheCommaSwap(t *testing.T) {
	roster := rosterOf("Ferguson, Mary", "Pat Mc Donald", "Chris Crew", "Sam Mc Donald", "Dana J. Driver")
	for typed, want := range map[string]string{
		"Mary Ferguson": "pa", // the comma swap
		"Pat McDonald":  "pb", // Mc Donald ×2 in the legacy data
		"Dana Driver":   "pe", // the middle initial
	} {
		got := personMatchCandidates(roster, typed, "")
		if len(got) != 1 || got[0].Person.PersonID != want {
			var ids []string
			for _, m := range got {
				ids = append(ids, m.Person.PersonID)
			}
			t.Errorf("%q offered %v, want exactly [%s]", typed, ids, want)
		}
	}
}

// TestNameFoldStepsInIsolation pins each fold step on its own input, in
// both keys, so a change to one step shows up as one row.
func TestNameFoldStepsInIsolation(t *testing.T) {
	cases := []struct{ in, firstLast, glued, gluedAll, step string }{
		{"Ferguson, Mary", "mary ferguson", "maryferguson", "maryferguson", "1 comma swap"},
		{"Ferguson,Mary", "mary ferguson", "maryferguson", "maryferguson", "1 comma swap without a space"},
		{"Ferguson, Mary, Jane", "ferguson jane", "fergusonmaryjane", "fergusonmaryjane", "1 two commas: joined as typed, no swap"},
		{"Pat Mc Donald", "pat donald", "patmcdonald", "patmcdonald", "2 spacing: glued carries it, first-last does not"},
		{"Mary Ann Smith", "mary smith", "maryannsmith", "maryannsmith", "2 a split first name glues"},
		{"Pat J Mc Donald", "pat donald", "patmcdonald", "patjmcdonald", "2 a middle initial is skipped by glued and kept by gluedAll"},
		{"Pat O Brien", "pat brien", "patbrien", "patobrien", "2 a lone O: skipped by glued as an initial, kept by gluedAll as a particle — only gluedAll meets \"Pat O'Brien\""},
		{"O Brien, Pat", "pat brien", "patbrien", "patobrien", "2 the same after the comma swap"},
		{"Mary J. Ferguson", "mary ferguson", "maryferguson", "maryjferguson", "3 initial: dropped by firstLast and glued, kept by gluedAll"},
		{"Mary Jane Ferguson", "mary ferguson", "maryjaneferguson", "maryjaneferguson", "3 middle name: first-last drops it, glued keeps it"},
		{"O'Brien, Pat", "pat obrien", "patobrien", "patobrien", "4 apostrophe vanishes"},
		{"Pat O’Brien", "pat obrien", "patobrien", "patobrien", "4 curly apostrophe vanishes"},
		{"Smith-Jones, Pat", "pat jones", "patsmithjones", "patsmithjones", "4 hyphen is a space"},
		{"Pat (Patty) Passenger", "pat passenger", "patpattypassenger", "patpattypassenger", "4 brackets are spaces; the nickname is a middle word"},
		{"José García", "josé garcía", "joségarcía", "joségarcía", "4 a precomposed accent is a letter, kept as is (NOT folded)"},
		{"José García", "jose garcia", "josegarcia", "josegarcia", "4 a decomposed accent's combining mark does not split the word"},
		{"Pat Passenger Jr", "pat passenger", "patpassenger", "patpassenger", "5 suffix word"},
		{"Pat Passenger, Sr.", "pat passenger", "patpassenger", "patpassenger", "5 suffix segment"},
		{"Passenger, Pat, Jr", "pat passenger", "patpassenger", "patpassenger", "5 suffix segment after Last, First"},
		{"Passenger, Pat Jr", "pat passenger", "patpassenger", "patpassenger", "5 suffix inside the first segment"},
		{"Pat Passenger III", "pat passenger", "patpassenger", "patpassenger", "5 III"},
		{"Pat Jr", "pat jr", "patjr", "patjr", "5 kept when one word would remain"},
		{"  PAT   PASSENGER ", "pat passenger", "patpassenger", "patpassenger", "0 trim + case"},
		{"Pat", "pat", "pat", "pat", "0 one word"},
		{"Pat Passenger 2", "pat 2", "patpassenger2", "patpassenger2", "0 digits are kept"},
	}
	for _, c := range cases {
		k, ok := nameFold(c.in)
		if !ok {
			t.Errorf("step %s: nameFold(%q) found nothing to fold", c.step, c.in)
			continue
		}
		if k.firstLast != c.firstLast || k.glued != c.glued || k.gluedAll != c.gluedAll {
			t.Errorf("step %s: nameFold(%q) = {%q %q %q}, want {%q %q %q}", c.step, c.in, k.firstLast, k.glued, k.gluedAll, c.firstLast, c.glued, c.gluedAll)
		}
	}
	for _, in := range []string{"", "   ", ",", " , ", "---", "'", ", Jr"} {
		if k, ok := nameFold(in); ok {
			t.Errorf("nameFold(%q) = %+v, want nothing to fold", in, k)
		}
	}
}

func TestPhoneKeyFoldsFormattingAndRefusesToGuessOnShortNumbers(t *testing.T) {
	const want = "9075550163"
	for _, s := range []string{"(907) 555-0163", "907-555-0163", "907.555.0163", "9075550163", "1 907 555 0163", "+1 (907) 555-0163"} {
		if got := phoneKey(s); got != want {
			t.Errorf("phoneKey(%q) = %q, want %q", s, got, want)
		}
	}
	for _, s := range []string{"", "ext 12", "555-01", "n/a", "call the office"} {
		if got := phoneKey(s); got != "" {
			t.Errorf("phoneKey(%q) = %q, want \"\" (fewer than seven digits must not take part)", s, got)
		}
	}
	if phoneKey("907-555-0163") == phoneKey("206-555-0163") {
		t.Error("two area codes folded together")
	}
}

// TestASharedPhoneIsASignalInsideANameMatchAndNeverAMatchByItself is the
// measured inversion: crews share dispatch lines, so a number in common
// confirms nothing on its own — and inside a name match it is said, not
// decided.
func TestASharedPhoneIsASignalInsideANameMatchAndNeverAMatchByItself(t *testing.T) {
	str := func(s string) *string { return &s }
	roster := []domain.Person{
		{PersonID: "same-phone", Name: "Ferguson, Mary", Phone: str("(907) 555-0163")},
		{PersonID: "other-phone", Name: "ferguson, mary", Phone: str("907-555-0199")},
		{PersonID: "no-phone", Name: "FERGUSON, MARY"},
		{PersonID: "dispatch-line", Name: "Smith, Robert", Phone: str("9075550163")}, // shares the number, not the name
	}
	got := personMatchCandidates(roster, "Ferguson, Mary", "+1 907 555 0163")
	var ids []string
	for _, m := range got {
		ids = append(ids, m.Person.PersonID)
	}
	// Order is by roster name as stored (a plain byte compare: "FERGUSON,
	// MARY" < "Ferguson, Mary" < "ferguson, mary"), then id.
	if want := "no-phone same-phone other-phone"; strings.Join(ids, " ") != want {
		t.Fatalf("candidates = %v, want %q (by roster name, then id) — and NEVER the shared-number stranger", ids, want)
	}
	if got[0].SamePhone || got[0].DifferentPhone {
		t.Errorf("no-phone should carry no phone signal at all: %+v", got[0])
	}
	if !got[1].SamePhone || got[1].DifferentPhone {
		t.Errorf("same-phone: %+v", got[1])
	}
	if got[2].SamePhone || !got[2].DifferentPhone {
		t.Errorf("other-phone: %+v", got[2])
	}
	// The gate's shape — no phone typed — says nothing about phones.
	for _, m := range personMatchCandidates(roster, "Ferguson, Mary", "") {
		if m.SamePhone || m.DifferentPhone {
			t.Errorf("no phone typed, yet %s carries a phone signal", m.Person.PersonID)
		}
	}
}
