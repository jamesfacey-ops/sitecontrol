// person_match_report.go — the asking rule (person_match.go) answered for a
// consumer that COUNTS instead of asks: cmd/dupreport's name-fold census,
// which sizes how often this system's own two entry conventions ("Last,
// First" at intake, "First Last" at the gate) produced two roster records
// for one human. The legacy data could not size that class (person_match.go,
// fold 1: one entry form, so its reversed-order rule added zero groups), so
// the only way to know is to count it in the new system from its first day.
//
// NOTHING HERE IS A SECOND SPELLING OF THE RULE. Reads is samePerson, Exact
// is sameName, and the keys are nameFold's own — called, not copied — so
// what the census reports is what the gate runs, and a change to the fold
// changes both at once. TestNameMatchReportReadsExactlyWhatTheAskingRuleOffers
// walks person_match_test.go's officerVariants table and asserts Reads
// equals the table's offer column, row for row and in both directions.
//
// WHICH FOLD DID THE JOINING is derived from the rule's own outputs, never
// from a second parse of the name:
//   - CommaSwap: fold 1 was NECESSARY — the pair no longer reads alike when
//     every comma is read as a space (the same samePerson, on the de-comma'd
//     spellings) — AND exactly one side was swapped by it (its first-and-last
//     key is not the key of its de-comma'd spelling). Both sides in intake
//     form ("O Brien, Pat" / "Brien, Pat") is therefore NOT this class: the
//     swap applied to both and joined nothing. The class is the
//     two-CONVENTION pair the census exists to count, and only that.
//   - Shape: which of nameFold's three keys agreed, named for what that
//     means to a reader. The one-letter-middle-word shape is where the rule's
//     stated price lives (person_match.go fold 4: "Pat O Brien" is offered
//     for "Pat Brien" because the O reads as an initial) — and so does the
//     real middle-initial case ("Mary J Ferguson" / "Mary Ferguson"). The
//     rule cannot tell them apart; a glance can. The census reports the
//     shape so a human can, and does not pretend to.
package web

import "strings"

// NameMatchReport is one pair of names under the asking rule, for counting.
type NameMatchReport struct {
	// Reads is samePerson: the rule would offer either for the other.
	Reads bool
	// Exact is sameName alone — case and whitespace. The strict key adopts a
	// trimmed, case-folded spelling, so two roster records that are Exact
	// were minted past it (inner whitespace, or a door without the key).
	Exact bool
	// CommaSwap: fold 1 did the joining and the two sides are in different
	// conventions. The class this file exists to count.
	CommaSwap bool
	// Shape names which of nameFold's keys agreed; "" when Exact or !Reads.
	Shape NameMatchShape
}

// NameMatchShape is the key signature of a loose match, named for a reader.
type NameMatchShape string

const (
	// ShapeSameLetters: all three keys agree — the names differ only by case,
	// punctuation, the comma, spacing BETWEEN words, or a suffix.
	ShapeSameLetters NameMatchShape = "same letters"
	// ShapeSpacing: both run-together keys agree and first-and-last does not
	// — spacing INSIDE a word ("Mc Donald"/"McDonald", "Mary Ann"/"Maryann").
	ShapeSpacing NameMatchShape = "spacing inside a word"
	// ShapeOneLetterMiddle: first-and-last and the skipping key agree, the
	// keeping key does not — one side has a one-letter middle word the other
	// lacks. An initial ("Mary J Ferguson") or a particle ("Pat O Brien"):
	// THE PRICE SHAPE, and the rule cannot say which.
	ShapeOneLetterMiddle NameMatchShape = "one-letter middle word dropped (initial or particle: the price shape)"
	// ShapeMiddleName: only first-and-last agrees — a middle name of two or
	// more letters on one side ("Mary Jane Ferguson"/"Mary Ferguson").
	ShapeMiddleName NameMatchShape = "middle name dropped"
	// ShapeParticleKept: only the keeping key agrees — "Pat O'Brien"/"Pat O
	// Brien", the miss fold 2's kept form exists for.
	ShapeParticleKept NameMatchShape = "one-letter particle kept"
	// ShapeInitialAndSpacing: only the skipping key agrees — an initial
	// dropped AND spacing inside a word ("Pat J Mc Donald"/"Pat McDonald").
	ShapeInitialAndSpacing NameMatchShape = "initial dropped and spacing inside a word"
	// ShapeOneLetterWordsRegrouped: first-and-last and the keeping key agree,
	// the skipping key does not — the same letters, with one-letter middle
	// words split or joined differently ("Pat A B Smith"/"Pat AB Smith").
	// Contrived; named so no signature is ever printed as a blank.
	ShapeOneLetterWordsRegrouped NameMatchShape = "one-letter middle words regrouped"
)

// ReportNameMatch answers for one pair. Symmetric in a and b.
func ReportNameMatch(a, b string) NameMatchReport {
	r := NameMatchReport{Reads: samePerson(a, b)}
	if !r.Reads {
		return r
	}
	if sameName(a, b) {
		r.Exact = true
		return r
	}
	// Reads and not Exact: samePerson joined them through nameFold, so both
	// folds are ok — the rule's own contract, not an assumption made here.
	ka, _ := nameFold(a)
	kb, _ := nameFold(b)
	r.Shape = shapeOf(ka.firstLast == kb.firstLast, ka.glued == kb.glued, ka.gluedAll == kb.gluedAll)
	r.CommaSwap = commaSwapped(a) != commaSwapped(b) && !samePerson(decomma(a), decomma(b))
	return r
}

func shapeOf(firstLast, glued, gluedAll bool) NameMatchShape {
	switch {
	case firstLast && glued && gluedAll:
		return ShapeSameLetters
	case !firstLast && glued && gluedAll:
		return ShapeSpacing
	case firstLast && glued && !gluedAll:
		return ShapeOneLetterMiddle
	case firstLast && !glued && !gluedAll:
		return ShapeMiddleName
	case !firstLast && !glued && gluedAll:
		return ShapeParticleKept
	case !firstLast && glued && !gluedAll:
		return ShapeInitialAndSpacing
	case firstLast && !glued && gluedAll:
		return ShapeOneLetterWordsRegrouped
	}
	// All three false cannot reach here (samePerson joined them through one
	// of the keys); named rather than blank so a future fourth key that
	// joins a pair on its own is visible in the census as exactly that.
	return "joined by a key this census does not name"
}

// commaSwapped reports whether fold 1 changed the reading of s — its
// first-and-last key is not the key of s with every comma read as a space.
// Detected by the fold's EFFECT, through nameFold itself, so the question
// "was this name in the comma form?" is never answered by a second parse:
// a suffix segment ("Pat Passenger, Sr.") and a two-comma name are not
// swapped by the rule, and read as not swapped here for the same reason.
func commaSwapped(s string) bool {
	with, okW := nameFold(s)
	without, okWo := nameFold(decomma(s))
	return okW && okWo && with.firstLast != without.firstLast
}

// decomma is the counterfactual spelling: the same name with fold 1
// unavailable to it, because no comma is left to read as a swap.
func decomma(s string) string { return strings.ReplaceAll(s, ",", " ") }
