// w8_compare_set_derived_test.go — W8-13(a), RE-ASSERTED AGAINST THE DERIVED
// SET. This file replaces a HOLLOW CHECK; the hollowness is the reason it
// exists, so it is recorded here rather than in a report only.
//
// ⛔ WHAT WAS HOLLOW [MEASURED at integration/witness-2026-09-05 @ 5a20b070, by
// `git show <ref>:<path>`, never by grepping a working tree]:
// internal/domain/w8_person_trade_test.go asserted W8-13(a) by reading
// cmd/dumpcompare/main.go AS TEXT and requiring the whole-file substring
// `trade`. At that ref:
//
//   - `trade` occurred in main.go EXACTLY ONCE, at line 95, INSIDE A COMMENT
//     reading "…asymmetry is why `trade` being on this line is asserted rather
//     than assumed."
//   - the quoted form `"trade"` occurred ZERO times.
//   - `compareSet`, the literal that check named, NO LONGER EXISTED — ⟦LOCK —
//     §67, 2026-09-03⟧ replaced it with buildCompareSet().
//
// ⭐⭐ SO THE CHECK PASSED BECAUSE OF A COMMENT DESCRIBING THE CHECK. Deleting
// `trade` from internal/domain/register.go, or adding `trade` to
// ignoredProjections, LEFT IT GREEN. The underlying property still held — only
// the instrument was hollow, which is the harder failure to see.
//
// ⚠ THE ORIGINAL AUTHOR RECORDED THIS SHORTFALL IN TERMS and could not close
// it: "W8-13(a)'s PRODUCED comparison … CANNOT BE WRITTEN FROM HERE: it needs a
// test in `cmd/dumpcompare`, and this pass may write only
// app/internal/**/w8_*_test.go. Recorded as a SHORTFALL, not silently dropped."
// This file is that test. It adds NO package: cmd/dumpcompare is one of the
// packages `go list ./...` already reports. ⟦was: "one of the nine" — nine was
// the module when this was written; G15 (2026-09-09) landed cmd/dupreport as
// the tenth. The claim this sentence makes — that this file costs no new
// package — is unchanged.⟧
//
// ⭐ WHY HERE AND NOT IN internal/domain. buildCompareSet, ignoredProjections
// and w67Ignored are unexported members of `package main`. An honest derived
// assertion from internal/domain would have to either export them or re-type
// the list — and a second hand-kept copy of the list is the SAME hollowness
// moved one package over. In-package access needs neither, and adds no exported
// API to a command binary. Precedent: witness_y13_q13_test.go already calls
// buildCompareSet() and mutates ignoredProjections directly.
//
// ⚠ WHAT THESE CHECKS DO NOT COVER, STATED AGAINST THEMSELVES:
//
//  1. VERSION SKEW IS INVISIBLE HERE, AND IT IS THE ONE LIVE FALSE-PASS. The
//     set is derived from THIS build's registry. If a deployed post runs an
//     older binary with no `trade` projection, `trade` is missing from BOTH
//     dumps, run() takes the !okA && !okB arm, WARNS TO STDERR and EXITS 0.
//     These checks pass and the two-post assert still did not compare trade.
//     Nothing in this package can see that; only reading the posts' binaries
//     can. Recorded, not closed.
//  2. They say nothing about whether the trade FOLD is correct, or whether the
//     dump emits trade rows faithfully — only that a trade difference between
//     two dumps is DETECTED.
//  3. W8-13(b)'s site — deploy/demo/demo-asserts.sh's keyfield list — is a
//     shell script with no derived value to read, and is NOT covered here.
package main

import (
	"strings"
	"testing"
)

// w8TradeProjection is the projection W8 added. It is named once, here, because
// the criterion is about THIS name; every assertion below resolves it against a
// value the tool DERIVES, never against source text.
const w8TradeProjection = "trade"

// w8DeletedProjections are the two W8 removed. ⚠ Their absence from the tree is
// already asserted by the byte censuses W8-6 and W8-9(c)
// [MEASURED — internal/domain/w8_deletions_test.go:264, :690]. What is asserted
// here is the narrower, derived thing those censuses cannot see: that neither is
// a MEMBER of the set this tool actually compares.
var w8DeletedProjections = []string{"workgroup", "ref_alias"}

// TestW8TradeIsAMemberOfTheDerivedCompareSet is W8-13(a), positive half.
//
// ⭐ THE LOAD-BEARING ASSERTION IS MEMBERSHIP IN buildCompareSet()'s OUTPUT.
// The registry and exclusion checks above it are DIAGNOSTIC ONLY — membership
// already implies both, since the set IS the registry minus the exclusions.
// They exist so a red says WHICH of the two ways it broke. ⛔ None of them can
// turn a failed membership into a pass: every arm is a t.Error or a t.Fatal.
func TestW8TradeIsAMemberOfTheDerivedCompareSet(t *testing.T) {
	// --- diagnostic (i): is it registered at all? -------------------------
	registered, err := registeredProjections()
	if err != nil {
		t.Fatalf("INSTRUMENT BROKEN: registeredProjections() failed: %v — the registry could not be built, so "+
			"W8-13(a) was NOT measured. This is an instrument fault, not a criterion red", err)
	}
	if !w8Contains(registered, w8TradeProjection) {
		t.Errorf("W8-13(a) DIAGNOSTIC: %q is NOT REGISTERED. The registry this binary builds the same way "+
			"`sitectl -dump` does (domain.Register + deadline.Register) reports %d projections and %q is not "+
			"among them: %s\n"+
			"⚠ A projection that is not registered cannot be in the compare set, so the two posts are never "+
			"compared on it and dumpcompare still exits 0",
			w8TradeProjection, len(registered), w8TradeProjection, strings.Join(registered, ", "))
	}

	// --- diagnostic (ii): is it excluded? ---------------------------------
	if reason, excluded := ignoredProjections[w8TradeProjection]; excluded {
		t.Errorf("W8-13(a) DIAGNOSTIC: %q is NAMED IN ignoredProjections, reason %q — it is deliberately "+
			"excluded from the compare set.\n"+
			"⚠ An excluded projection falls into `ignored`, prints \"ignored (not in the §13.3 compare set)\" "+
			"to STDOUT, and the tool STILL EXITS 0. The trade register would never be compared between posts",
			w8TradeProjection, reason)
	}
	if w67Ignored(w8TradeProjection) {
		t.Errorf("W8-13(a) DIAGNOSTIC: w67Ignored(%q) is true — it is excluded by NAME or by a CLASS SUFFIX "+
			"rule in ignoredProjectionClasses (%d rule(s) declared). Same consequence: never compared, exit 0",
			w8TradeProjection, len(ignoredProjectionClasses))
	}

	// --- THE CRITERION: membership in the set the tool actually builds ----
	set, err := buildCompareSet()
	if err != nil {
		t.Fatalf("W8-13(a): buildCompareSet() REFUSED: %v\n"+
			"⚠ run() treats this as FATAL and returns 2 — \"a tool that cannot establish what SHOULD be compared "+
			"must not report on what does compare\". So the §13.3 step-6 assert does not run at all", err)
	}
	if !w8Contains(set, w8TradeProjection) {
		t.Errorf("W8-13(a) ((m8-33)): %q is NOT A MEMBER of the compare set buildCompareSet() produces. "+
			"The derived set has %d member(s): %s\n"+
			"⚠ THIS IS THE FALSE GREEN THE OLD CHECK COULD NOT SEE: an absent key falls into `ignored`, prints "+
			"\"ignored (not in the §13.3 compare set)\" to STDOUT, and the run STILL EXITS 0. The stale-key "+
			"direction is caught by the byte censuses (W8-6, W8-9(c)); a MISSING key fires no census at all, and "+
			"two dumps compare IDENTICALLY whether or not the trade register agrees between the posts",
			w8TradeProjection, len(set), strings.Join(set, ", "))
	}

	// --- the negative half: the two W8 deleted are not compared -----------
	for _, gone := range w8DeletedProjections {
		if w8Contains(set, gone) {
			t.Errorf("W8-13(a) ((m8-32)): %q is a MEMBER of the derived compare set, and W8 DELETED that "+
				"projection.\n"+
				"⚠ Being in the set means it is expected in both dumps. Absent from both, run() takes the "+
				"!okA && !okB arm, warns to STDERR, CONTINUES, and prints \"IDENTICAL: … compared projections "+
				"match\" AND RETURNS 0 — the determinism check passes while counting a projection that is gone",
				gone)
		}
	}
}

// TestW8ATradeDifferenceBetweenTwoDumpsIsDETECTED is W8-13(a)'s PRODUCED half —
// the comparison the original author recorded as a shortfall.
//
// ⭐ THIS IS THE PROPERTY ITSELF, NOT A PROXY FOR IT: two dumps that disagree on
// exactly one `trade` row must make the tool FAIL. Membership in a derived slice
// is strong evidence for that; running the tool is the thing itself.
//
// ⚠ THE FIXTURES CARRY AN IDENTICAL `company` ROW ON PURPOSE. Without it, a
// build in which `trade` is not compared would produce ZERO compared rows, trip
// D-85's VACUOUS refusal and exit 2 — a red, but for the wrong reason. With it
// the run is non-vacuous, so a build that does not compare `trade` prints
// "IDENTICAL" and EXITS 0. ⭐ That is the exact production harm, reproduced.
func TestW8ATradeDifferenceBetweenTwoDumpsIsDETECTED(t *testing.T) {
	const dumpTradeA = `{
		"company":[{"company_id":"site-a-company-1","name":"Acme"}],
		"trade":[{"trade_id":"t-known","name":"Scaffolding"}]
	}`
	const dumpTradeB = `{
		"company":[{"company_id":"site-a-company-1","name":"Acme"}],
		"trade":[{"trade_id":"t-known","name":"Scaffolding-DIVERGED"}]
	}`

	code, out, errOut := runTool(t, saved(t, dumpTradeA), saved(t, dumpTradeB))
	if code != 1 {
		t.Errorf("W8-13(a) PRODUCED ((m8-33)): two dumps differing in exactly one `trade` row exited %d, want 1.\n"+
			"⚠ EXIT 0 MEANS THE TRADE REGISTER IS NOT COMPARED BETWEEN THE POSTS: the key falls into `ignored`, "+
			"the identical `company` row keeps the run non-vacuous, and the tool prints IDENTICAL over a real "+
			"disagreement. Exit 2 instead means buildCompareSet refused, or the run was judged VACUOUS because "+
			"nothing at all was compared.\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "DIFF trade") {
		t.Errorf("W8-13(a) PRODUCED: the output does not NAME `trade` as the differing projection — an officer "+
			"reading the §13.3 sheet cannot act on a diff that does not say which register disagreed.\n"+
			"stdout:\n%s", out)
	}
	if !strings.Contains(out, "Scaffolding-DIVERGED") {
		t.Errorf("W8-13(a) PRODUCED: the output does not SHOW the differing row content.\nstdout:\n%s", out)
	}
	if strings.Contains(out, "IDENTICAL") {
		t.Errorf("W8-13(a) PRODUCED: the tool printed IDENTICAL over two dumps that genuinely disagree on a "+
			"`trade` row.\nstdout:\n%s", out)
	}
}

// w8Contains is an identity test over the DERIVED slice — not a substring
// match. ⛔ strings.Contains would make `trade` satisfiable by `trade_amendment`
// and re-introduce a weaker form of the defect this file exists to close.
func w8Contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
