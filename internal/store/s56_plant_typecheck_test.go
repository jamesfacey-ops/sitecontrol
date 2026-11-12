package store

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// ⟦LOCK — W5-POSTLOCK-AMENDMENTS.md §56(1), 2026-09-03, pass S102⟧
//
// ⭐ THE DURABLE HALF OF §56, landed for every plant this package runs.
//
// Every source scan in this package type-checks the sources it walks. Each
// one used to do it like this:
//
//	types.Config{Error: func(error) {}, …}
//
// ⚠ THAT DISCARDS EVERY DIAGNOSTIC go/types RAISES. A plant that does not
// type-check then produces NO HIT — which is byte-identical to what a BLIND
// SCANNER produces — and the harness printed "the planted violation was NOT
// detected" for both. A DEAD PLANT (a fixture defect) and a BLIND SCANNER
// (a real, serious finding) were rendered IDENTICALLY, and the wording sent
// the next reader to debug the one part that was working.
//
// ⚠⚠ §61 measured what that cost in the ONE harness anybody had looked at:
// FIVE of eight positive controls could not fail, and EVERY ONE READ GREEN.
//
// THE TWO STREAMS, kept apart because they answer different questions and
// [MEASURED 2026-09-03] one of them is NEVER empty on a healthy tree:
//
//	Imports      "could not import X" — the DESIGNED consequence of these
//	             scans running with `Importer: nil` (or `importer.Default()`,
//	             which cannot supply a module-local package). On the tracked
//	             tree this is 2..24 per directory and that is not rot. A
//	             required-zero bar here would red every scan permanently.
//	Substantive  every OTHER diagnostic — the source itself does not
//	             type-check. ⭐ ON A PLANT THIS MUST BE ZERO, and [MEASURED]
//	             it is zero for every plant in this package today. On the
//	             tracked tree it is NOT zero (cascades of the failed
//	             imports: `missing type in composite literal`, `missing
//	             return`, `X undefined`), so the tree side RECORDS and
//	             REPORTS rather than bars.
//
// ⚠ HONEST LIMIT, stated so nobody reads this fix as wider than it is: with
// `Importer: nil` a type error that rides on an UNRESOLVABLE IMPORT is
// masked by the import failure itself and lands in neither stream. The bar
// below is exact for a plant that breaks on a PACKAGE-LOCAL symbol — which
// is the shape every dead plant §61 found actually had (a renamed local
// argument, a deleted local type).
type s56Diags struct {
	Where       string // the directory that was type-checked
	Imports     []string
	Substantive []string
}

// Record is the replacement for the discarded `func(error) {}`.
func (d *s56Diags) Record(err error) {
	msg := err.Error()
	if strings.Contains(msg, "could not import ") {
		d.Imports = append(d.Imports, msg)
		return
	}
	d.Substantive = append(d.Substantive, msg)
}

// s56Set is every directory one scan type-checked. A scan that walks a tree
// checks several, and a plant may land in ANY of them, so the bar is taken
// over the whole set and never over one directory.
type s56Set []s56Diags

func (s s56Set) Substantive() []string {
	var out []string
	for _, d := range s {
		for _, m := range d.Substantive {
			out = append(out, d.Where+": "+m)
		}
	}
	sort.Strings(out)
	return out
}

// Report renders the diagnostics for a failure message, so a reader who is
// told "not detected" can see AT THE SAME MOMENT whether the tree under the
// scan type-checked. ⚠ This is the half that stops the misdirection §56
// found; the bar below is the half that stops the silence.
func (s s56Set) Report() string {
	if len(s) == 0 {
		return "  (no directory was type-checked — the scan resolved NOTHING)"
	}
	var b strings.Builder
	for _, d := range s {
		fmt.Fprintf(&b, "  %s: %d import-resolution, %d substantive", d.Where, len(d.Imports), len(d.Substantive))
		for _, m := range d.Substantive {
			b.WriteString("\n      SUBSTANTIVE: " + m)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// s56PlantTypeChecked is THE NAMED FAILURE §56(1) requires — its own words,
// "the plant did not compile". It returns false, after reporting the plant BY
// NAME and quoting the diagnostics, when the planted source did not compile.
//
// ⚠ "COMPILE" rather than "type-check" because the substantive stream carries
// BOTH: a go/types diagnostic, and (where the scan records it) a file that did
// not PARSE. A parse failure never reaches go/types, so a bar phrased around
// type-checking alone would miss the door §56's defect can also arrive by.
//
// ⚠ CALL IT BEFORE READING THE HITS, and skip the hit assertion when it
// returns false. The whole defect §56 names is a harness that goes on to say
// "the planted violation was NOT detected" about a plant that never
// compiled.
func s56PlantTypeChecked(t *testing.T, control string, set s56Set) bool {
	t.Helper()
	bad := set.Substantive()
	if len(bad) == 0 {
		return true
	}
	t.Errorf("⛔ THE PLANT DID NOT COMPILE — %s ⟦§56(1)⟧\n"+
		"  This is a FIXTURE DEFECT, not a detector failure. The control below was NOT run, because a plant "+
		"that does not compile produces no hit, and a no-hit reads identically to a blind scanner.\n"+
		"  ⚠ Do NOT debug the scan. Repair or re-aim the plant, then re-run.\n"+
		"  diagnostics (%d):\n    %s", control, len(bad), strings.Join(bad, "\n    "))
	return false
}

// s56LogTree records what a REAL-TREE scan's type-check produced. It is a
// calibration line, not a bar: see the note on the two streams above.
func s56LogTree(t *testing.T, scan string, set s56Set) {
	t.Helper()
	var imports, substantive int
	for _, d := range set {
		imports += len(d.Imports)
		substantive += len(d.Substantive)
	}
	t.Logf("⟦§56⟧ %s type-checked %d director(ies): %d import-resolution diagnostics, %d substantive",
		scan, len(set), imports, substantive)
}
