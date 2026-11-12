// Tests named for what they prove (BUILDER_BRIEF_RULES 4/5), same convention
// as cmd/dumpcompare/main_test.go.
//
// The two load-bearing ones are TestSeededDuplicatePairIsCaught (proves this
// tool CAN fire) and TestCleanNonDegenerateSetProducesNoFindingsAndStatesTheCount
// (proves it can go clean on a real, non-trivial input rather than merely on
// an empty one) — together they are this tool's own version of dumpcompare's
// §219 lesson: "TWO EMPTY DUMPS AGREE" must never be the only way this tool
// knows how to say CLEAN. TestEmptyInputIsLabeledNotSilentlyClean and
// TestWrongShapedFileIsAParseErrorNotAnEmptyPass are the direct proof that an
// empty or malformed input cannot be mistaken, by a reader of the output,
// for a real clean pass over real data.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// person builds one "person" row. Trailing fields are omitted (not needed by
// this tool — see personAmendmentDumpRow's doc comment, in
// internal/domain/dump_compose.go, on which facts it reads).
// ⟦was: "wireAmendmentRow" — that type was deleted in the W3-14 refactor
// (e6c2e67), which moved the dump-file compose entry point into the domain
// package; personAmendmentDumpRow is its live counterpart. The sentence was
// made INCOMPLETE by the move, not false: the pointer needed a new target,
// not deletion.⟧
func person(id, name string) string {
	return `{"person_id":"` + id + `","site_id":"site-a","name":"` + name + `"}`
}

// visit builds one "visit" row. company/trade snapshot fields are passed as
// `""` for "not recorded" (encoded as JSON null) or a real string.
func visit(id, site, personID string, open int, company, trade, personSnap string) string {
	str := func(s string) string {
		if s == "" {
			return "null"
		}
		return `"` + s + `"`
	}
	return `{"visit_id":"` + id + `","site_id":"` + site + `","person_id":"` + personID + `",` +
		`"trip_id":null,"company_snapshot_id":null,"company_snapshot_name":` + str(company) + `,` +
		`"trade_snapshot_id":null,"trade_snapshot_name":` + str(trade) + `,` +
		`"person_snapshot_name":` + str(personSnap) + `,` +
		`"is_open":` + itoa(open) + `,"recorded_by":null}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

// amendment builds one "person_amendment" row asserting same_as (the only
// amendment shape these tests need).
func amendment(seq int, personID, sameAs string) string {
	return `{"site_id":"site-a","node_epoch":1,"seq":` + itoaN(seq) + `,"person_id":"` + personID + `",` +
		`"name":null,"same_as":"` + sameAs + `","cleared":null}`
}

func itoaN(n int) string {
	digits := "0123456789"
	if n < 10 {
		return string(digits[n])
	}
	return string(digits[n/10]) + string(digits[n%10])
}

func dump(visits, persons, amendments []string) string {
	return `{"visit":[` + strings.Join(visits, ",") + `],` +
		`"person":[` + strings.Join(persons, ",") + `],` +
		`"person_amendment":[` + strings.Join(amendments, ",") + `]}`
}

func saved(t *testing.T, body string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "dump.json")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func runTool(t *testing.T, files ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = run(files, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestSeededDuplicatePairIsCaught is the proof this tool CAN fire: two open
// visits, two DIFFERENT person_ids, names that fold to the same key (case +
// whitespace differ, exactly the class textFoldKey exists for), same
// company/trade. Must exit 1 and name both visits and both person_ids.
func TestSeededDuplicatePairIsCaught(t *testing.T) {
	d := dump(
		[]string{
			visit("v1", "site-a", "pA1", 1, "Acme", "Electrician", ""),
			visit("v2", "site-b", "pA2", 1, "Acme", "Electrician", ""),
		},
		[]string{person("pA1", "John Smith"), person("pA2", "JOHN  SMITH")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 1 {
		t.Fatalf("seeded duplicate pair: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("want a DUPLICATE? block, got:\n%s", out)
	}
	for _, want := range []string{"v1", "v2", "pA1", "pA2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q named in the finding, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "LIKELY DUPLICATES: 1 groups found") {
		t.Fatalf("want the verdict line naming 1 group, got:\n%s", out)
	}
}

// TestCleanNonDegenerateSetProducesNoFindingsAndStatesTheCount is the other
// half of the proof: a real, non-trivial input (6 open visits, all distinct
// people) must go clean at exit 0 — and the printed denominator must show
// the REAL count (6), not just an exit code, so this test cannot be
// satisfied by an instrument that is silently broken and finding nothing
// regardless of input. See TestEmptyInputIsLabeledNotSilentlyClean for the
// contrasting degenerate case.
func TestCleanNonDegenerateSetProducesNoFindingsAndStatesTheCount(t *testing.T) {
	names := []string{"Alice Adams", "Bob Baker", "Carol Chen", "Dave Diaz", "Erin Evans", "Frank Ford"}
	var visits, persons []string
	for i, n := range names {
		id := "p" + string(rune('1'+i))
		visits = append(visits, visit("v"+string(rune('1'+i)), "site-a", id, 1, "Acme", "Electrician", ""))
		persons = append(persons, person(id, n))
	}
	d := dump(visits, persons, nil)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("clean non-degenerate set: want exit 0, got %d, out:\n%s", code, out)
	}
	if strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("want no DUPLICATE? blocks, got:\n%s", out)
	}
	if !strings.Contains(out, "examined: 6 open visits") {
		t.Fatalf("want the real count (6) in the denominator line, got:\n%s", out)
	}
	if !strings.Contains(out, "CLEAN: no likely duplicate identities among 6 open visits") {
		t.Fatalf("want the CLEAN verdict naming 6, got:\n%s", out)
	}
}

// TestEmptyInputIsLabeledNotSilentlyClean is dumpcompare's "TWO EMPTY DUMPS
// AGREE" lesson applied here: an empty (but well-formed) dump must still
// exit 0 — there is genuinely nothing to flag — but its denominator line
// must say 0, in terms, so it can never be confused with the non-degenerate
// clean pass above by a reader of the output.
func TestEmptyInputIsLabeledNotSilentlyClean(t *testing.T) {
	d := dump(nil, nil, nil)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("empty input: want exit 0, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "examined: 0 open visits") {
		t.Fatalf("want the denominator to say 0 in terms, got:\n%s", out)
	}
	if !strings.Contains(out, "CLEAN: no likely duplicate identities among 0 open visits") {
		t.Fatalf("want the CLEAN verdict naming 0, got:\n%s", out)
	}
}

// TestCannotSeeCaveatIsAlwaysPrinted pins the one line of this tool's output
// that no test named before this one: the caveat that a zero in the census
// above means "no second record exists", not "the class does not occur" —
// nameFoldCensus prints it unconditionally, before either the exit-0 or
// exit-1 return in run(). Proved on the emptiest possible input (nothing to
// examine at all) on purpose: if it prints here it prints on every real run
// too, and this is the fixture least likely to carry it via some other path.
func TestCannotSeeCaveatIsAlwaysPrinted(t *testing.T) {
	code, out, _ := runTool(t, saved(t, dump(nil, nil, nil)))
	if code != 0 {
		t.Fatalf("empty input: want exit 0, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "cannot see: a duplicate the offer PREVENTED left no second record") {
		t.Fatalf("want the cannot-see caveat printed unconditionally, got:\n%s", out)
	}
}

// TestWrongShapedFileIsAParseErrorNotAnEmptyPass is the other half of the
// same lesson from the opposite direction: a syntactically valid JSON file
// that is NOT a dump (missing the "visit"/"person" sections entirely, e.g.
// a stray "{}" saved by mistake) must be a loud exit-2 parse refusal, not a
// silent "0 open visits, CLEAN" — that failure mode would make a broken
// capture indistinguishable from a genuinely quiet site.
func TestWrongShapedFileIsAParseErrorNotAnEmptyPass(t *testing.T) {
	code, out, errOut := runTool(t, saved(t, "{}"))
	if code != 2 {
		t.Fatalf("wrong-shaped file: want exit 2, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(errOut, "missing \"visit\" or \"person\" section") {
		t.Fatalf("want the missing-section refusal, got stderr:\n%s", errOut)
	}
	if strings.Contains(out, "CLEAN") {
		t.Fatalf("a parse error must never also print a CLEAN verdict, got:\n%s", out)
	}
}

// TestAlreadyLinkedViaSameAsIsExcludedNotFlagged: the same duplicate-looking
// pair as the seeded-catch test, but an existing person_amendment already
// asserts same_as linking pA2 to pA1 — an officer has ALREADY resolved this
// one. It must NOT be re-reported, and the exclusion must be counted and
// named in the denominator.
func TestAlreadyLinkedViaSameAsIsExcludedNotFlagged(t *testing.T) {
	d := dump(
		[]string{
			visit("v1", "site-a", "pA1", 1, "Acme", "Electrician", ""),
			visit("v2", "site-b", "pA2", 1, "Acme", "Electrician", ""),
		},
		[]string{person("pA1", "John Smith"), person("pA2", "John Smith")},
		[]string{amendment(1, "pA2", "pA1")},
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("already-linked pair: want exit 0, got %d, out:\n%s", code, out)
	}
	if strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("an already-linked identity must not be re-reported, got:\n%s", out)
	}
	if !strings.Contains(out, "excluded: 1 person_ids already resolve to another via an existing same_as link") {
		t.Fatalf("want the exclusion counted and named, got:\n%s", out)
	}
}

// TestClosedVisitsAreNotExamined: a same-name pair exists, but one of the
// two visits is CLOSED (is_open:0) — muster (§11) is a view over open
// visits only, and this backstop must honor the same rule: a closed visit
// contributes nothing to the pool, so with only one open member the name
// group cannot be a finding.
func TestClosedVisitsAreNotExamined(t *testing.T) {
	d := dump(
		[]string{
			visit("v1", "site-a", "pA1", 1, "Acme", "Electrician", ""),
			visit("v2", "site-b", "pA2", 0, "Acme", "Electrician", ""), // closed
			visit("v3", "site-a", "pB1", 1, "Acme", "Plumber", ""),     // unrelated filler
		},
		[]string{person("pA1", "John Smith"), person("pA2", "John Smith"), person("pB1", "Jane Doe")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("closed half of the pair: want exit 0, got %d, out:\n%s", code, out)
	}
	if strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("a closed visit must not count toward a finding, got:\n%s", out)
	}
	if !strings.Contains(out, "examined: 2 open visits") {
		t.Fatalf("want only the 2 OPEN visits counted (v2 excluded), got:\n%s", out)
	}
}

// TestCompanyMismatchDoesNotSuppressButIsNoted: same name, no same_as link,
// but the two visits recorded DIFFERENT companies. Per the file comment,
// company/trade are corroborating context only and must never suppress a
// name match — the pair is still reported, and the mismatch is called out
// as DISAGREES rather than hidden.
func TestCompanyMismatchDoesNotSuppressButIsNoted(t *testing.T) {
	d := dump(
		[]string{
			visit("v1", "site-a", "pA1", 1, "Acme", "", ""),
			visit("v2", "site-b", "pA2", 1, "Baker Inc", "", ""),
		},
		[]string{person("pA1", "Robert Stone"), person("pA2", "Robert Stone")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 1 {
		t.Fatalf("company mismatch must not suppress the match: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("want the pair still reported despite the company mismatch, got:\n%s", out)
	}
	if !strings.Contains(out, "company: DISAGREES") {
		t.Fatalf("want the mismatch called out as DISAGREES, got:\n%s", out)
	}
}

// TestUnionAcrossMultipleFiles: the seeded pair split across two SEPARATE
// dump files (one visit+person each), passed as two arguments — proves the
// union step (this tool's departure from dumpcompare's fixed two-argument
// pairwise shape) actually merges rather than only ever examining the last
// file given.
func TestUnionAcrossMultipleFiles(t *testing.T) {
	fileA := dump(
		[]string{visit("v1", "site-a", "pA1", 1, "Acme", "Electrician", "")},
		[]string{person("pA1", "John Smith")},
		nil,
	)
	fileB := dump(
		[]string{visit("v2", "site-b", "pA2", 1, "Acme", "Electrician", "")},
		[]string{person("pA2", "John Smith")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, fileA), saved(t, fileB))
	if code != 1 {
		t.Fatalf("union of two files: want exit 1, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, "examined: 2 open visits") {
		t.Fatalf("want both files' visits unioned (2 total), got:\n%s", out)
	}
	if !strings.Contains(out, "v1") || !strings.Contains(out, "v2") {
		t.Fatalf("want both visits named in the finding, got:\n%s", out)
	}
}

func TestUsageErrorIsExit2(t *testing.T) {
	code, out, errOut := runTool(t)
	if code != 2 {
		t.Fatalf("no arguments: want exit 2, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(errOut, "usage: dupreport") {
		t.Fatalf("want a usage message, got stderr:\n%s", errOut)
	}
}

// TestNonDumpHTMLBodyIsAnErrorNotAnEmptyPass mirrors dumpcompare's own trap
// test: a saved HTML page (e.g. a login redirect captured by mistake) must
// be a loud parse refusal, never quietly read as an empty, clean dump.
func TestNonDumpHTMLBodyIsAnErrorNotAnEmptyPass(t *testing.T) {
	code, out, errOut := runTool(t, saved(t, "<!doctype html><html><body>login</body></html>"))
	if code != 2 {
		t.Fatalf("HTML body: want exit 2, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(errOut, "not a projection dump") {
		t.Fatalf("want a loud parse refusal, got stderr:\n%s", errOut)
	}
}

// TestADumpURLIsRefused pins the same D-56 boundary dumpcompare pins: this
// tool has no network code path either (it links net/http only transitively,
// through internal/web, for the name-fold census's one pure function), and
// an operator with an old URL in their notes must be told what to run
// instead.
func TestADumpURLIsRefused(t *testing.T) {
	code, out, errOut := runTool(t, "http://10.90.1.10:8081/debug/projection-dump")
	if code != 2 {
		t.Fatalf("URL argument: want exit 2, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(errOut, "no longer exist") || !strings.Contains(errOut, "sitectl -config") {
		t.Fatalf("want the refusal to name the replacement command, got stderr:\n%s", errOut)
	}
}

// --- the name-fold census (nameFoldCensus) ---------------------------------
//
// The census is a COUNT over the roster, never a finding: every test below
// also asserts that the verdict line and the exit code are exactly what the
// open-visit half alone produces, so the instrument can never be mistaken
// for a guard. The fixtures are roster-only where the point is the roster:
// a comma-swap pair with NO open visit for either record must still count,
// or the census would be a second muster check rather than the roster
// question it exists to answer.

const (
	censusCommaSwapLabel = "the swap did the joining — the class only this count can size): "
	censusExactLabel     = "exact (case and whitespace only): "
	censusOtherLabel     = "other folds (spacing, middle name or initial, punctuation, suffix): "
)

// TestCensusCountsACommaSwapRosterPairAndNamesBothRecords is the proof the
// census CAN count the class it exists for: intake's "Ferguson, Mary" and the
// gate's "Mary Ferguson" as two roster records, no open visit for either, one
// unrelated person on site. The pair counts under comma-swap with both ids
// named; the verdict stays CLEAN at exit 0.
func TestCensusCountsACommaSwapRosterPairAndNamesBothRecords(t *testing.T) {
	d := dump(
		[]string{visit("v1", "site-a", "pD1", 1, "Acme", "Electrician", "")},
		[]string{person("pA1", "Ferguson, Mary"), person("pA2", "Mary Ferguson"), person("pD1", "Dana Driver")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("a census count must not move the exit code: want 0, got %d, out:\n%s", code, out)
	}
	for _, want := range []string{
		"CLEAN: no likely duplicate identities among 1 open visits",
		censusCommaSwapLabel + "1 unmerged, 0 already merged by an officer",
		censusExactLabel + "0 unmerged, 0 already merged by an officer",
		censusOtherLabel + "0 unmerged, 0 already merged by an officer",
		`pA1 "Ferguson, Mary" (site-a) / pA2 "Mary Ferguson" (site-a) [same letters]`,
		"census: 3 named persons (0 unnamed skipped), 3 identities after existing merges, 3 pairs compared, 1 pairs read alike",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DUPLICATE?") {
		t.Fatalf("a census pair must never print as a muster finding, got:\n%s", out)
	}
}

// TestCensusCountsNothingOnUnrelatedNamesAndStatesThePairCount: six different
// people, two of them in intake form, and one record with no name at all.
// Every class reads 0, and the denominator still shows the real pair count
// (15) and the skipped record, so a zero can never be mistaken for an
// instrument that compared nothing.
func TestCensusCountsNothingOnUnrelatedNamesAndStatesThePairCount(t *testing.T) {
	names := []string{"Alice Adams", "Baker, Bob", "Carol Chen", "Diaz, Dave", "Erin Evans", "Frank Ford", ""}
	var persons []string
	for i, n := range names {
		persons = append(persons, person("p"+string(rune('1'+i)), n))
	}
	code, out, _ := runTool(t, saved(t, dump(nil, persons, nil)))
	if code != 0 {
		t.Fatalf("want exit 0, got %d, out:\n%s", code, out)
	}
	for _, want := range []string{
		censusCommaSwapLabel + "0 unmerged, 0 already merged by an officer",
		censusExactLabel + "0 unmerged, 0 already merged by an officer",
		censusOtherLabel + "0 unmerged, 0 already merged by an officer",
		"census: 6 named persons (1 unnamed skipped), 6 identities after existing merges, 15 pairs compared, 0 pairs read alike",
		"a glance can): 0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q, got:\n%s", want, out)
		}
	}
}

// TestCensusCountsAnOfficerMergedCommaSwapPairApartAndDoesNotListIt: the
// same comma-swap pair, but a person_amendment already links pA2 to pA1 — a
// human closed it. It counts under "already merged", not "unmerged", and the
// pair line does not print: a duplicate the system caught and a human closed
// is not the open class this count sizes.
func TestCensusCountsAnOfficerMergedCommaSwapPairApartAndDoesNotListIt(t *testing.T) {
	d := dump(nil,
		[]string{person("pA1", "Ferguson, Mary"), person("pA2", "Mary Ferguson")},
		[]string{amendment(1, "pA2", "pA1")},
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("want exit 0, got %d, out:\n%s", code, out)
	}
	if !strings.Contains(out, censusCommaSwapLabel+"0 unmerged, 1 already merged by an officer") {
		t.Fatalf("want the merged pair counted apart, got:\n%s", out)
	}
	if strings.Contains(out, `pA1 "Ferguson, Mary"`) {
		t.Fatalf("a pair an officer already merged must not be listed, got:\n%s", out)
	}
	if !strings.Contains(out, "census: 2 named persons (0 unnamed skipped), 1 identities after existing merges, 1 pairs compared, 1 pairs read alike") {
		t.Fatalf("want the denominator to show 2 records resolving to 1 identity, got:\n%s", out)
	}
}

// TestCensusReportsThePriceShapeApartFromTheCommaSwap: the rule's stated
// price row ("Pat O Brien"/"Pat Brien", internal/web/person_match_test.go)
// beside a genuine comma-swap pair. The price pair counts under other folds
// with the price shape named and the price tally reads 1; comma-swap reads
// exactly the one genuine pair, and the price pair is listed under other
// folds, not under comma-swap — so a reader can tell a catch from the price
// without re-deriving either.
func TestCensusReportsThePriceShapeApartFromTheCommaSwap(t *testing.T) {
	d := dump(nil,
		[]string{person("pA1", "Ferguson, Mary"), person("pA2", "Mary Ferguson"), person("pC1", "Pat O Brien"), person("pC2", "Pat Brien")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("want exit 0, got %d, out:\n%s", code, out)
	}
	priceLine := `pC1 "Pat O Brien" (site-a) / pC2 "Pat Brien" (site-a) [one-letter middle word dropped (initial or particle: the price shape)]`
	for _, want := range []string{
		censusCommaSwapLabel + "1 unmerged, 0 already merged by an officer",
		censusOtherLabel + "1 unmerged, 0 already merged by an officer",
		priceLine,
		"a glance can): 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q, got:\n%s", want, out)
		}
	}
	// Placement: the price pair sits under the other-folds heading, after
	// the comma-swap section has closed.
	swapAt, exactAt, otherAt, priceAt := strings.Index(out, censusCommaSwapLabel), strings.Index(out, censusExactLabel), strings.Index(out, censusOtherLabel), strings.Index(out, priceLine)
	fergusonAt := strings.Index(out, `pA1 "Ferguson, Mary"`)
	if !(swapAt < fergusonAt && fergusonAt < exactAt && exactAt < otherAt && otherAt < priceAt) {
		t.Fatalf("the genuine pair must be listed under comma-swap and the price pair under other folds, got:\n%s", out)
	}
}

// TestCensusDoesNotCountTwoIntakeSpellingsAsTheCommaSwapClass: "O Brien, Pat"
// and "Brien, Pat" are joined by the initial skip with the swap applied to
// BOTH — one convention, not two. Comma-swap reads 0; the pair counts under
// other folds. This is the attribution boundary that decides whether the
// count answers "did the two conventions do this" or merely "was there a
// comma somewhere".
func TestCensusDoesNotCountTwoIntakeSpellingsAsTheCommaSwapClass(t *testing.T) {
	d := dump(nil, []string{person("pA1", "O Brien, Pat"), person("pA2", "Brien, Pat")}, nil)
	code, out, _ := runTool(t, saved(t, d))
	if code != 0 {
		t.Fatalf("want exit 0, got %d, out:\n%s", code, out)
	}
	for _, want := range []string{
		censusCommaSwapLabel + "0 unmerged, 0 already merged by an officer",
		censusOtherLabel + "1 unmerged, 0 already merged by an officer",
		`pA1 "O Brien, Pat" (site-a) / pA2 "Brien, Pat" (site-a) [one-letter middle word dropped (initial or particle: the price shape)]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q, got:\n%s", want, out)
		}
	}
}

// TestCensusNeverMovesAnExit1VerdictEither is the other direction of "not a
// guard": the seeded open-visit duplicate (exit 1) with a comma-swap roster
// pair beside it. The verdict line and exit code are the open-visit half's
// own — the census adds one count and moves nothing.
func TestCensusNeverMovesAnExit1VerdictEither(t *testing.T) {
	d := dump(
		[]string{
			visit("v1", "site-a", "pA1", 1, "Acme", "Electrician", ""),
			visit("v2", "site-b", "pA2", 1, "Acme", "Electrician", ""),
		},
		[]string{person("pA1", "John Smith"), person("pA2", "JOHN  SMITH"), person("pF1", "Ferguson, Mary"), person("pF2", "Mary Ferguson")},
		nil,
	)
	code, out, _ := runTool(t, saved(t, d))
	if code != 1 {
		t.Fatalf("the open-visit duplicate sets exit 1 with or without a census count: got %d, out:\n%s", code, out)
	}
	for _, want := range []string{
		"LIKELY DUPLICATES: 1 groups found",
		censusCommaSwapLabel + "1 unmerged, 0 already merged by an officer",
		censusExactLabel + "1 unmerged, 0 already merged by an officer", // John Smith / JOHN  SMITH on the roster too
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q, got:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "this tool never merges") {
		t.Fatalf("the verdict must stay the LAST line, after the census, got:\n%s", out)
	}
}
