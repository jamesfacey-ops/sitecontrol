// w9b_carrier_coverage_test.go — W9b-4 (VF §7.7, F4 §8): the carrier-coverage
// test ENUMERATES `arrival_ref_retracted` and exempts it with the written
// cancellation-shape reason — never silently absent.
//
// ⚠ THE FILTER IS THE FINDING, and it is why this criterion is not a one-line
// membership check. VF §7.7 fixes the enumeration source as "the v10
// replicated event-type inventory (never a hand-kept list)" and fixes the
// exemption row as "the cancellation-shaped non-carriers: `alert_ack_retracted`
// … joined by F4's `arrival_ref_retracted`". The DELIVERED enumeration is
// W3-12's pre-bump classification — a `_amended`/`_corrected` suffix filter
// [MEASURED — `w3AmendmentFamilyTypes`, `internal/domain/w3_outcome_test.go`] —
// and `arrival_ref_retracted` ends in NEITHER. So on the delivered filter the
// retraction types are not "unregistered", they are INVISIBLE, and W4's own
// judgement then reports an exemption row naming them as a FAILURE ("names a
// type ABSENT from the enumeration source"). W4's `w4_registry_test.go` uses
// exactly that fact as its CONTROL 2, with `alert_ack_retracted` as the
// planted bogus row.
//
// That is VF §7.7's second named failure shape in as many words: the test
// "silently narrows its enumeration — the exact silent exclusion this section
// exists to forbid". W9b's coverage check therefore widens the classification
// to the RETRACTION family as well, and the widening is asserted, not assumed:
// the narrow-filter control below shows the delivered filter missing the type,
// so a builder who leaves the filter alone reds here rather than going green
// on a coverage test that never looked.
//
// SEAM W9B-S-EXEMPT, bound HERE, once: the exemption list is the package var
// `w4ExemptNonCarriers`, which is the var W4's own `⟦LOCK — TE-1⟧` comment
// names ("`w4ExemptNonCarriers` is EMPTY at W4 … the §7.7 exemption rows
// (`alert_ack_retracted`, `arrival_ref_retracted`) land WITH W9's retraction
// types"). A builder who puts the rows somewhere else takes it to the GATE.
//
// ⚠ THIS TEST ASSERTS A W4 LOCK AMENDMENT. Adding the row AMENDS
// `⟦LOCK — TE-1⟧`. The amendment is scheduled by the lock's own comment and is
// W9's to make, but it is an amendment, and W9b's gate must record it AS one —
// see `reports/window-build/W9b-tests.md`. The last assertion in this file is
// the mechanical half of that: a list that has grown rows while its comment
// still claims it is EMPTY is a lock amended silently.
//
// ⚠ THE PAIRED ROW IS W9c's. W9b-4: "It lands together with W9c's row." Both
// rows are asserted here because that is what the criterion says; a build that
// lands one and not the other reds with a message naming the other sub-chunk.
package domain

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w9bEvtAckRetracted is W9c's type. Named here only because W9b-4's own text
// names it: the two exemption rows land together.
const w9bEvtAckRetracted = "alert_ack_retracted"

// w9bCarrierClassSuffixes is the classification WIDENED to reach the
// retraction family. Stated as a suffix property over the log — a derivation,
// not a hand-kept membership list, which is the shape VF §7.7 requires
// ("never a hand-kept list") and the shape that survives W9c and anything
// after it adding another `_retracted`.
var w9bCarrierClassSuffixes = []string{"_amended", "_corrected", "_retracted"}

// w9bDeliveredClassSuffixes is the classification as DELIVERED at W4. Kept so
// the narrow-filter control can be produced rather than asserted in prose.
var w9bDeliveredClassSuffixes = []string{"_amended", "_corrected"}

// w9bInventory enumerates the distinct payload types on the given streams
// whose names carry one of the supplied suffixes — the "v10 replicated
// event-type inventory", filtered.
func w9bInventory(t *testing.T, st *store.Store, suffixes []string, streams [][2]any) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, s := range streams {
		site := s[0].(string)
		epoch := s[1].(int64)
		for _, e := range w9bEventsOf(t, st, site, epoch) {
			typ := w9bPayloadType(e)
			for _, suf := range suffixes {
				if strings.HasSuffix(typ, suf) {
					seen[typ] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for typ := range seen {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

func w9bContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestW9b4RetractionIsEnumeratedAndExemptedWithAWrittenReason is W9b-4.
func TestW9b4RetractionIsEnumeratedAndExemptedWithAWrittenReason(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-e", "occ-e")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "COVERAGE TRUCK", "drv-e", []string{"occ-e"}, 240)
	a1 := w9bArriveExpected(t, st, "site-b", 1, x.TripID)

	// A LIVE log carrying BOTH retraction types — W9b's own and the paired
	// W9c row, which W9b-4 says lands with it.
	w9bApplyNext(t, st, "site-a", 1, w9bBase+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))
	w9bApplyNext(t, st, "site-a", 1, w9bBase+2*w9bMinute, map[string]any{
		"type": w9bEvtAckRetracted, "target_site_id": "site-a", "target_node_epoch": 1, "target_seq": 1,
	})

	streams := [][2]any{{"site-a", int64(1)}, {"site-b", int64(1)}}

	// --- THE NARROW-FILTER CONTROL, produced before anything else counts --
	// The delivered `_amended`/`_corrected` classification must be shown to
	// MISS the retraction types. If it did not, the widening below would be
	// decoration and a coverage test could keep the old filter and stay green
	// while enumerating nothing about W9's types.
	narrow := w9bInventory(t, st, w9bDeliveredClassSuffixes, streams)
	if w9bContains(narrow, w9bEvtRefRetracted) || w9bContains(narrow, w9bEvtAckRetracted) {
		t.Fatalf("W9b-4 CONTROL FAILED: the DELIVERED `%v` classification already reaches the retraction types (inventory = %v). "+
			"This control exists to prove the widening below is load-bearing; if the narrow filter already sees them, the whole silent-narrowing argument of VF §7.7 needs re-deriving, not assuming",
			w9bDeliveredClassSuffixes, narrow)
	}

	// --- the WIDENED enumeration source reaches both types ---------------
	inv := w9bInventory(t, st, w9bCarrierClassSuffixes, streams)
	for _, typ := range []string{w9bEvtRefRetracted, w9bEvtAckRetracted} {
		if !w9bContains(inv, typ) {
			t.Fatalf("W9b-4: the widened enumeration source does not reach %q (inventory = %v). "+
				"VF §7.7: the source is the v10 replicated event-type inventory, never a hand-kept list, and a source that cannot see the type makes the exemption row a DEAD row",
				typ, inv)
		}
	}

	// --- the exemption list names BOTH rows ------------------------------
	// SEAM W9B-S-EXEMPT. This is the assertion that REDS on the pre-chunk
	// tree: `w4ExemptNonCarriers` is `[]string{}` at W4 by its own lock.
	for _, typ := range []string{w9bEvtRefRetracted, w9bEvtAckRetracted} {
		if !w9bContains(w4ExemptNonCarriers, typ) {
			owner := "W9b"
			if typ == w9bEvtAckRetracted {
				owner = "W9c — W9b-4: \"It lands together with W9c's row\""
			}
			t.Fatalf("W9b-4: `w4ExemptNonCarriers` = %v and does not name %q (owner: %s).\n"+
				"VF §7.7 fixes exactly one exemption row, the cancellation-shaped non-carriers, and F4 §8 registers NOTHING for this event: it carries no value slots and nothing for `Outcome` to resolve. "+
				"Adding the row AMENDS the W4 `⟦LOCK — TE-1⟧` on this var — scheduled by the lock's own comment, W9's to make, and W9b's gate must record it AS a lock amendment",
				w4ExemptNonCarriers, typ, owner)
		}
	}

	// --- the retraction is NOT a registered carrier ----------------------
	// The exemption's own claim, checked rather than trusted: exempting a
	// type that IS registered would be the opposite error.
	for _, typ := range []string{w9bEvtRefRetracted, w9bEvtAckRetracted} {
		if IsRegisteredCarrier(typ) {
			t.Fatalf("W9b-4: %q is a REGISTERED carrier and is also on the exemption list. VF §7.7's row is for non-carriers; a registered type must discharge its visibility through `Outcome`, not be exempted from doing so", typ)
		}
	}

	// --- the coverage judgement is clean, on the same judgement W4 runs ---
	if problems := w4CoverageProblems(inv, w4ExemptNonCarriers, IsRegisteredCarrier); len(problems) != 0 {
		t.Fatalf("W9b-4: coverage problems over the live log: %v.\ninventory = %v\nexemptions = %v", problems, inv, w4ExemptNonCarriers)
	}

	// --- THE DEAD-ROW CONTROL (W4's both-directions judgement) -----------
	// The rows are legal only because the types are LIVE in the enumeration
	// source. Against a log holding neither, the same judgement must report
	// them as failures — never a skip, never a dormant row.
	dead := w4CoverageProblems([]string{"person_amended"}, []string{w9bEvtRefRetracted, w9bEvtAckRetracted}, func(string) bool { return true })
	if len(dead) != 2 {
		t.Fatalf("W9b-4 DEAD-ROW CONTROL FAILED: against an inventory holding neither retraction type, the coverage judgement reported %v, want a FAILURE for each of the two exemption rows. "+
			"W4's judgement calls a dead exemption row \"a FAILURE, never a skip, never a dormant row\"; a check that tolerates one is the silently-narrowing shape", dead)
	}
}

// --- the WRITTEN reason ---------------------------------------------------

var w9bExemptDeclRe = regexp.MustCompile(`(?m)^var\s+w4ExemptNonCarriers\s*=`)

// TestW9b4TheExemptionRowCarriesItsWrittenReason is W9b-4's "with the written
// cancellation-shape reason — never silently absent" half, plus the mechanical
// half of the lock-amendment obligation.
//
// The reason is a SOURCE-level property: VF §7.7 and F4 §8 require the row to
// carry a written reason, and nothing in the Go type of a `[]string` can hold
// one. So the declaration site is located by scan (never by a hardcoded path —
// a builder may move the var) and its attached comment block is read.
//
// ⚠ WORDING DEPENDENCE, DECLARED. This asserts that the comment carries the
// word "cancellation" and at least one of the grounds the two designs give
// ("no value slots" / "nothing for `Outcome` to resolve" / "no winner"). A
// builder who writes an equally good reason in different words reds here and
// should take it to the GATE rather than editing this file. That dependence is
// reported in `W9b-tests.md` as a shortfall of this check, not hidden.
func TestW9b4TheExemptionRowCarriesItsWrittenReason(t *testing.T) {
	file, line := w9bFindExemptDeclaration(t)
	comment := w9bCommentAbove(t, file, line)
	lower := strings.ToLower(comment)

	if !strings.Contains(lower, w9bEvtRefRetracted) {
		t.Fatalf("W9b-4: the comment attached to `w4ExemptNonCarriers` (%s:%d) does not name %q.\ncomment:\n%s\n"+
			"VF §7.7: the row is named and reasoned, never silently absent", file, line, w9bEvtRefRetracted, comment)
	}
	if !strings.Contains(lower, "cancellation") {
		t.Fatalf("W9b-4: the written reason at %s:%d does not describe the row as CANCELLATION-shaped.\ncomment:\n%s\n"+
			"W9b-4: \"exempts it with the written cancellation-shape reason\"; F4 §3.6 and F6 §5 both rest the exemption on the ratified shape test", file, line, comment)
	}
	grounds := []string{"no value slot", "nothing for `outcome` to resolve", "nothing for outcome to resolve", "no winner"}
	found := false
	for _, g := range grounds {
		if strings.Contains(lower, g) {
			found = true
		}
	}
	if !found {
		t.Fatalf("W9b-4: the written reason at %s:%d names the shape but not the GROUND — none of %v appears.\ncomment:\n%s\n"+
			"F4 §8 / VF §7.7: the event carries no value slots and nothing for `Outcome` to resolve, which is WHY registering it would invent per-fact resolution where none exists. "+
			"A reason that states the conclusion without the ground is the row's name written twice",
			file, line, grounds, comment)
	}

	// --- the LOCK-AMENDMENT self-contradiction guard ---------------------
	if len(w4ExemptNonCarriers) > 0 && strings.Contains(comment, "EMPTY at W4") {
		t.Fatalf("W9b-4: `w4ExemptNonCarriers` now holds %v while the comment at %s:%d still asserts it \"is EMPTY at W4 ⟦LOCK — TE-1⟧\". "+
			"That is a locked criterion's test amended SILENTLY — the amendment is scheduled by the lock's own comment, but it is an amendment, and both the comment and W9b's gate record must say so",
			w4ExemptNonCarriers, file, line)
	}
}

// w9bFindExemptDeclaration locates the `w4ExemptNonCarriers` declaration by
// scanning every Go file in this package's directory — never a hardcoded path,
// so moving the var relocates the check instead of silently disabling it.
func w9bFindExemptDeclaration(t *testing.T) (string, int) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("w9b: reading the package directory: %v", err)
	}
	var hits []string
	var hitFile string
	var hitLine int
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("w9b: reading %s: %v", e.Name(), err)
		}
		for i, ln := range strings.Split(string(b), "\n") {
			if w9bExemptDeclRe.MatchString(ln) {
				hits = append(hits, e.Name())
				hitFile, hitLine = e.Name(), i+1
			}
		}
	}
	if len(hits) == 0 {
		t.Fatalf("W9b-4: no `var w4ExemptNonCarriers =` declaration found in package domain. SEAM W9B-S-EXEMPT names that var as the exemption list, following W4's own `⟦LOCK — TE-1⟧` comment; a build that put the rows elsewhere takes it to the GATE")
	}
	if len(hits) > 1 {
		t.Fatalf("W9b-4: `w4ExemptNonCarriers` is declared in %v — more than one exemption list means the coverage test and this check can be reading different ones", hits)
	}
	return hitFile, hitLine
}

// w9bCommentAbove returns the contiguous `//` comment block immediately above
// the given 1-based line.
func w9bCommentAbove(t *testing.T, file string, line int) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("w9b: reading %s: %v", file, err)
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	for i := line - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "//") {
			break
		}
		out = append([]string{trimmed}, out...)
	}
	return strings.Join(out, "\n")
}
