// person_match.go — THE ONE "does this person read like somebody already on
// the roster?" rule, asked at every door that CREATES a person: intake's
// enrolment form (intake.go), the gate's add-on-demand row (gate.go's
// decorateRow) and the gate's post-commit advisory (dupadvisory.go).
//
// THE RULE ONLY EVER ASKS. A candidate is SHOWN to the officer beside the
// record's company and trade; the officer picks it or ignores it; the row
// commits as typed either way ([[D-66]]: the gate never refuses; intake
// refuses only until the officer affirms "different person"). The STRICT
// key that silently adopts a typed name — gate.go's resolveTypedPerson,
// TrimSpace + EqualFold, exactly one hit — is a different function and is
// widened by NOTHING here: person_match_test.go walks every row of its
// variant table through resolveTypedPerson and asserts it adopts exactly
// the spellings it always did, and person_dup_test.go's non-resolution
// guard pins the same at the door.
//
// THAT ASYMMETRY SETS THE WIDTH. A false candidate costs one glance. A
// missed duplicate costs a permanently split roster record: the live muster
// folds company and trade merges and NEVER the person key
// (TestW6TheWebMusterReadPathNeverResolvesThePersonKey), so a duplicate that
// gets minted stays two records for good, and prevention at the door is the
// only defence the board has. Being loose here is cheap; being tight is
// expensive. The previous width — picker.go's sameName alone, case and
// whitespace — was MEASURED (reports/witness/REVIEW-PERSON-DUP.md, 22
// officer variants at 72d6111) to offer on exactly ONE spelling the strict
// key did not already adopt, and to miss all three of the extra duplicates
// the legacy data actually holds (reports/witness/PERSON-DUP-RATE.md:
// "Mc Donald"/"McDonald" ×2, a middle initial ×1). The legacy rate curve is
// flat (3.47% exact → 3.69% loosest) BECAUSE exact matches are already
// caught by the strict key; that delta is the loose rule's entire job, and
// it is measured on one entry form — the variant this system's own two
// conventions manufacture (fold 1 below) could not appear in it at all.
//
// THE FOLDS — deterministic, one sentence each, each pinned in isolation by
// TestNameFoldStepsInIsolation. Two names read alike when sameName says so,
// or when nameFold gives them the same first-and-last key or the same
// run-together key in either of its two forms. NOT a similarity engine: no
// edit distance, no phonetics.
//
//  1. THE COMMA SWAP. A name with exactly one comma is "Last, First" and is
//     read as "First Last". This system MANUFACTURES the variant: intake
//     composes "Last, First" (intake.go's composeName) and the gate box takes
//     the spoken "First Last" — MEASURED at 72d6111 to mint a duplicate with
//     no offer and no advisory at all three doors. The legacy data could not
//     size it: one entry form, so its reversed-order rule adds zero groups.
//  2. SPACING. All the words are run together, so "Mc Donald" reads as
//     "McDonald", "Van der Berg" as "Vanderberg" and "Mary Ann" as "Maryann".
//     A one-letter word between the first and last is compared BOTH ways —
//     skipped, so "Pat J Mc Donald" reads as "Pat McDonald" (an initial), and
//     kept, so "Pat O Brien" reads as "Pat O'Brien" (a surname particle) —
//     because nothing in the name says which it is. Two of the legacy data's
//     three real extra duplicates.
//  3. MIDDLE NAMES AND INITIALS. Only the first and last words are compared,
//     so "Mary J. Ferguson", "Mary Jane Ferguson" and "Mary Ferguson" read
//     alike. One real legacy case — and the same two-convention mechanism as
//     the comma swap: an ID card at intake carries the full name, the gate
//     hears the spoken one.
//  4. PUNCTUATION. Apostrophes are dropped and every other mark reads as a
//     space, so "O'Brien", "OBrien" and "O Brien" agree — with a first name
//     in front as well ("Pat O'Brien"/"Pat O Brien", "O'Brien, Pat"/"O Brien,
//     Pat"), which is what the kept-word form in fold 2 is for: MEASURED at
//     9217dff, with the skipping form alone the lone O went as an initial,
//     "brien" was read as the surname, and the pair was a MISS while the
//     surname-only pair agreed. "Smith-Jones" and "Smith Jones" agree
//     likewise. The price, stated: "Pat O Brien" is ALSO offered for "Pat
//     Brien", because to the first-and-last key the O reads as an initial —
//     the O is visible in the offered name, and it asks. One extra legacy
//     group under the punctuation-stripped rule.
//  5. A GENERATIONAL SUFFIX. Jr/Sr/II/III/IV after the first word — as a word
//     or as its own comma segment — is ignored while two words remain, so
//     "Pat Passenger Jr", "Pat Passenger, Sr." and "Pat Passenger" agree.
//     This one WILL sometimes show a father when a son is typed: the suffix
//     stays visible in the offered name, the company and trade sit beside
//     it, and one glance settles it. The alternative is minting a third
//     record when one person was entered with and without the suffix.
//
// REJECTED, with the reason:
//   - reversing a name with NO comma ("Ferguson Mary" vs "Mary Ferguson"):
//     nothing marks the order, and "James Scott"/"Scott James" are two real
//     people. The comma is the signal, and intake always writes one.
//   - one-letter typos, edit distance, phonetics ("Dan"/"Don", "Jon"/"John"):
//     that is a similarity engine; it fires on genuinely different short
//     names, and no evidence supports it.
//   - nicknames ("Bob"/"Robert"): a table, not a fold; unmeasured.
//   - an initial standing for a first name ("J Smith"/"John Smith"): every
//     J. Smith on a crew would be offered for every other.
//   - accents ("José"/"Jose"): unmeasured, and the decomposition it needs
//     (golang.org/x/text/unicode/norm) would be the FIRST third-party import
//     in internal/web — MEASURED: the W6 muster witness type-checks this
//     package through importer.Default() and reported the tree as
//     un-type-checkable ("undefined: norm.NFD") the moment it was added. So
//     what is rejected is the PRECOMPOSED form, which is what browsers send
//     (NFC): "José" and "Jose" are different first words here. A DECOMPOSED
//     accent (e + combining acute) has its mark dropped rather than
//     splitting its word, and therefore DOES read as "Jose" — a guard for a
//     stray mark, not an accent fold, stated so "rejected" is not read as
//     "never".
//
// WHAT IT STILL MISSES, stated so nobody reads a clean pass as proof (each
// MEASURED by probe at 9217dff): the rejected shapes above; a first name
// joined against one split at the gate ("Maryann Smith"/"Mary Smith" — "Ann"
// reads as a middle name); a double surname against its FIRST half ("Maria
// Garcia Lopez"/"Maria Garcia" — the primary surname under Hispanic
// convention — is a miss, while "Maria Rodriguez Lopez", a different person,
// IS offered: this shape errs both ways); a hyphenated surname against its
// first half ("Pat Smith-Jones"/"Pat Smith" is a miss, while "Pat Jones" is
// offered); a trailing comma ("Passenger, Pat,"), which makes three segments;
// "Jnr", "Snr", "2nd" and "V", which are not recognised suffixes; two or more
// commas; a company typed into the name box; real spelling differences;
// anything on a peer post that has not replicated yet. The post-commit
// advisory misses the same set — it is this rule.
//
// THE PRECEDENT IS THE VEHICLE ONE (vehicle.go's vehicleFoldKey /
// VehiclesFoldingTo): a LOOSE key exists to ASK a human about
// near-duplicates and for nothing else — it never merges, never resolves,
// never reaches a column. The strict key stays where it is and stays what
// it is: keying resolution on any fold here would decide "Mary Ferguson" IS
// "Ferguson, Mary" without asking (vehicle.go names the same move mutant
// (m7-11)). sameName itself is picker.go's and is CALLED here, not copied
// and not changed: it also decides which same-name picker rows draw the
// tick-box discriminator, and that decoration must not widen with this.
//
// ⛔ WHY IT ONLY EVER ASKS. Two different humans folded into one record is a
// SAFETY DEFECT — the muster shows one person where two are on site, and one
// of them is nobody's job to find. A duplicate is untidy; a false merge can
// leave somebody behind in an evacuation. MEASURED on the legacy data: of 45
// exact-duplicate groups, 2 are strong signals of two different people
// sharing a name, 24 look like one person re-entered, and 19 (42%) cannot be
// told apart from the fields on file — so any automatic rule would be
// guessing on nearly half of real cases. Nothing here, and nothing that
// consumes this, auto-merges or auto-selects: candidates are SURFACED with
// their COMPANY (the one field that separated the two true different-people
// cases) and the officer chooses; every door that shows candidates still
// lets the officer add the person.
//
// THE PHONE IS A CORROBORATING SIGNAL INSIDE A NAME MATCH, NEVER A KEY.
// MEASURED: 58 distinct numbers are shared across two or more DIFFERENTLY
// named people — company and dispatch lines — so a shared number confirms
// nothing and is never a match on its own, and there is no uniqueness rule
// on it anywhere. Inside a name match it is worth a word: the SAME number
// leans "one person", a DIFFERENT number leans "two people". Both are said,
// neither decides. The comparison is on ASCII digits with a leading trunk
// "1" dropped and at least seven digits, or the number does not take part;
// the stored phone is never touched (W5b: verbatim).
package web

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"sitecontrol/internal/domain"
)

// personMatch is one roster person a typed name reads like, with the phone
// as a corroborating signal where BOTH sides have a usable number. Both
// flags false means the phone had nothing to say.
type personMatch struct {
	Person         domain.Person
	SamePhone      bool
	DifferentPhone bool
}

// personMatchCandidates returns every person in `persons` whose name reads
// as `name` under samePerson, ordered by roster name then id so the offer
// is stable across renders. phone may be "" — the gate collects none (D-52).
//
// It takes the roster rather than the store because every caller already
// holds the composed roster (domain.Persons) for the page it is rendering.
// It never writes, never resolves an id, and never consults a merge map: a
// merged-away duplicate still on the roster is still a candidate, and the
// commit path resolves a picked id through the merge snapshot like any
// other.
func personMatchCandidates(persons []domain.Person, name, phone string) []personMatch {
	return personMatchFolded(persons, foldNames(persons), name, phone)
}

// personMatchFolded is personMatchCandidates over a roster whose names were
// folded once already (foldNames) — the roster import asks it for every row
// of a file, and folding the whole roster again per row was most of a
// 2,000-row check's time (FIX17b). folded[i] is persons[i]'s name.
func personMatchFolded(persons []domain.Person, folded []foldedName, name, phone string) []personMatch {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	typed := foldName(name)
	pk := phoneKey(phone)
	var out []personMatch
	for i, p := range persons {
		if !folded[i].same(typed) {
			continue
		}
		m := personMatch{Person: p}
		if pk != "" && p.Phone != nil {
			if k := phoneKey(*p.Phone); k != "" {
				m.SamePhone, m.DifferentPhone = k == pk, k != pk
			}
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Person.Name != out[j].Person.Name {
			return out[i].Person.Name < out[j].Person.Name
		}
		return out[i].Person.PersonID < out[j].Person.PersonID
	})
	return out
}

// samePerson reports whether a typed name READS AS a roster name to an
// officer: picker.go's sameName (case and whitespace — the base fold, called
// not copied), or the same first-and-last key, or the same run-together key
// in either form under nameFold. It is the ASKING rule and nothing resolves
// through it.
func samePerson(roster, typed string) bool {
	return foldName(roster).same(foldName(typed))
}

// foldedName is a name with nameFold already applied, so one name can be
// compared with many (samePerson is exactly foldName(a).same(foldName(b))).
type foldedName struct {
	raw string
	key nameKey
	ok  bool
}

func foldName(s string) foldedName {
	k, ok := nameFold(s)
	return foldedName{raw: s, key: k, ok: ok}
}

func foldNames(persons []domain.Person) []foldedName {
	out := make([]foldedName, len(persons))
	for i, p := range persons {
		out[i] = foldName(p.Name)
	}
	return out
}

// same is samePerson's rule: picker.go's sameName, or any nameFold key.
func (a foldedName) same(b foldedName) bool {
	if sameName(a.raw, b.raw) {
		return true
	}
	return a.ok && b.ok && (a.key.firstLast == b.key.firstLast || a.key.glued == b.key.glued || a.key.gluedAll == b.key.gluedAll)
}

// nameKey is the three comparison forms of one name. Two names read alike
// when ANY form agrees; each form alone would miss a real case (firstLast
// misses "Mc Donald", glued misses "Mary Jane", and glued alone misses
// "Pat O Brien" against "Pat O'Brien").
type nameKey struct {
	// firstLast is the first word and the last word — "mary ferguson" for
	// "Ferguson, Mary J." — so a middle name or initial on either side does
	// not separate two entries of one person.
	firstLast string
	// glued is every word run together with one-letter middle initials
	// dropped — "patmcdonald" for "Pat J. Mc Donald" — so spacing inside a
	// name part does not separate them.
	glued string
	// gluedAll is every word run together with NOTHING dropped — "patobrien"
	// for "Pat O Brien" — for the one-letter word that is a surname particle
	// rather than an initial. MEASURED at 9217dff: with glued alone, "Pat
	// O'Brien"/"Pat O Brien" was a miss (the O skipped as an initial, "brien"
	// read as the surname) while the surname-only pair agreed. It joins
	// nothing glued or firstLast does not already join unless one side has a
	// one-letter middle word, so its price is the contrived pair only ("Mary
	// A Smith"/"Marya Smith") — and it asks.
	gluedAll string
}

// generationalSuffix is the set of words fold 5 ignores after the first
// word. "V" is left out: it reads as an initial.
var generationalSuffix = map[string]bool{"jr": true, "sr": true, "ii": true, "iii": true, "iv": true}

// nameFold is the fold, step by step in the order the file comment numbers
// them (punctuation first because the comma must survive it to be counted).
// ok is false when nothing that reads as a word is left.
func nameFold(s string) (k nameKey, ok bool) {
	// Fold 4: lower-case letters and digits only; apostrophes vanish, every
	// other mark becomes a space; the comma is kept for the next step. A
	// combining mark (a decomposed accent) also vanishes — NOT an accent
	// fold (see the file comment), only a guard so a mark never splits the
	// word it belongs to.
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\'' || r == '’' || r == '‘' || r == 'ʼ' || r == '`':
			// O'Brien → OBrien
		case unicode.Is(unicode.Mn, r):
			// a combining mark rides with its letter; it is not a word break
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == ',':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	// Fold 5, the segment form: a last comma segment that is only a suffix
	// ("Pat Passenger, Sr." / "Passenger, Pat, Jr") is dropped before the
	// commas are counted.
	parts := strings.Split(b.String(), ",")
	if last := len(parts) - 1; last >= 1 && generationalSuffix[strings.TrimSpace(parts[last])] {
		parts = parts[:last]
	}
	// Fold 1: exactly one comma is "Last, First"; read it as "First Last".
	// Any other count is joined as typed — two commas mark nothing this
	// rule understands.
	var joined string
	if len(parts) == 2 {
		joined = parts[1] + " " + parts[0]
	} else {
		joined = strings.Join(parts, " ")
	}
	words := strings.Fields(joined)
	// Fold 5, the word form: a suffix anywhere after the first word is
	// dropped, unless that would leave fewer than two words ("Pat Jr" keeps
	// both — nothing says which word is the surname).
	kept := words[:0:0]
	for i, w := range words {
		if i > 0 && generationalSuffix[w] {
			continue
		}
		kept = append(kept, w)
	}
	if len(kept) >= 2 {
		words = kept
	}
	if len(words) == 0 {
		return nameKey{}, false
	}
	// Fold 3: the first and last word.
	k.firstLast = words[0]
	if len(words) > 1 {
		k.firstLast += " " + words[len(words)-1]
	}
	// Fold 2: everything run together — once with one-letter middle words
	// skipped (an initial), once with them kept (a surname particle).
	var g, all strings.Builder
	for i, w := range words {
		all.WriteString(w)
		if i > 0 && i < len(words)-1 && utf8.RuneCountInString(w) == 1 {
			continue
		}
		g.WriteString(w)
	}
	k.glued, k.gluedAll = g.String(), all.String()
	return k, true
}

// phoneKey is the comparison form of a phone number, or "" when the number
// is too short to take part. ASCII digits only — a number in another script
// is not folded, it simply does not participate, which is the honest miss
// rather than a wrong signal.
func phoneKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 11 && d[0] == '1' {
		d = d[1:]
	}
	if len(d) < 7 {
		return ""
	}
	return d
}
