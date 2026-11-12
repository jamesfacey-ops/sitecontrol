// w4_registry_test.go — W4-13(a): occurred_at_corrected is the §4.4
// carrier registry's FIFTH carrier (fact vocabulary "occurred_at",
// canonical Asserted rendering, target-key extraction); an unregistered
// event type is an ERROR from Outcome, loud, never blank; the coverage
// test enumerates from the replicated event-type inventory FILTERED per
// W3-12's pre-bump classification (the `_amended`/`_corrected` families —
// stated once at W3-12, cited here), never a hand-kept list; the exemption
// list is EMPTY at W4 (the §7.7 rows land WITH W9's retraction types); and
// an exemption row naming a type absent from the enumeration source is
// itself a coverage-test FAILURE — never a skip, never a dormant row —
// with the planted-exemption-row control producing that red before the
// clean pass counts (the lock's CONTROL rows 2 and 6).
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANTS / CONTROLS: the planted unregistered type (control,
// CONTROL row 2) and the planted bogus exemption row (control, CONTROL
// row 6) — per §5, a scan with neither its own control nor a paired
// mutant does not pass.
package domain

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// w4PlantedCorrectionType is a carrier-CLASS name (`_corrected` suffix, so
// the W3-12 filter enumerates it) that is never registered.
const w4PlantedCorrectionType = "gadget_corrected"

// ⟦LOCK AMENDMENT — W9b (item 16) + W9c (item 17), 2026-09-02, SCHEDULED BY
// THIS LOCK'S OWN COMMENT.⟧ The superseded text declared this list to hold
// no rows at W4 under ⟦LOCK — TE-1⟧, and said the §7.7 exemption rows
// (`alert_ack_retracted`, `arrival_ref_retracted`) land WITH W9's retraction
// types because their written reasons are W9's designs and logically cannot
// precede W9's types. W9's types are here, so the rows land — together, as
// W9b-4 requires ("It lands together with W9c's row"), because this file's
// own judgement treats a dead exemption row as a FAILURE in both directions.
// ⟦was: that declaration, verbatim. It is NOT reproduced here: W9b-4's
// self-contradiction guard reds on a comment that still asserts the list
// holds nothing while it holds rows — which is the guard working. The exact
// superseded text is preserved in
// `reports/window-build/W9b-build.md` §7 instead.⟧
//
// ⚠ THIS IS AN AMENDMENT TO A LOCKED W4 TEST, not an ordinary edit, and
// W9b's gate must record it as one. It is also NOT the whole of the
// amendment W9b-4 schedules: see the build report §7, which reports the
// consequence for `TestW4CarrierCoverageWithEmptyExemptions` below and
// routes the remedy to the gate rather than choosing one here.
//
// THE WRITTEN REASON (VF §7.7; F4 §8; F6 §6) — never silently absent, and
// never the row's name written twice:
// `arrival_ref_retracted` and `alert_ack_retracted` are CANCELLATION-shaped
// — a targeted withdrawal of ONE recorded assertion, F6's algebra, not a
// value amendment. THE GROUND: they carry no value slots and nothing for
// `Outcome` to resolve. Nothing is contested, so there is no winner to name
// and no per-fact supersession to compute; registering either as a carrier
// would invent per-fact resolution where none exists, and put a fact
// vocabulary on an event that has none.
var w4ExemptNonCarriers = []string{"alert_ack_retracted", "arrival_ref_retracted"}

// w4CoverageProblems is the coverage judgement, driven by BOTH controls
// and the clean pass (one code path, so a control red proves the same
// path the clean pass runs): every inventoried type must be registered or
// exempt, and every exemption row must name a type PRESENT in the
// enumeration source — a dead exemption row is a FAILURE, never a skip.
func w4CoverageProblems(inventory, exemptions []string, registered func(string) bool) []string {
	inv := map[string]bool{}
	for _, typ := range inventory {
		inv[typ] = true
	}
	exempt := map[string]bool{}
	var problems []string
	for _, e := range exemptions {
		exempt[e] = true
		if !inv[e] {
			problems = append(problems, "exemption row "+e+" names a type ABSENT from the enumeration source — a coverage-test FAILURE, never a skip, never a dormant row (W4-13a, C-7's preserved evidence)")
		}
	}
	for _, typ := range inventory {
		if !exempt[typ] && !registered(typ) {
			problems = append(problems, "unregistered amendment/correction carrier "+typ+" — an unregistered type is an error from Outcome, loud, never blank (VF §4.4)")
		}
	}
	sort.Strings(problems)
	return problems
}

// TestW4FifthCarrierRegistered — W4-13(a)'s registration half: the carrier
// predicate answers true for occurred_at_corrected, and the three §4.4
// registration goods are observable through Outcome on a real correction:
// the fact vocabulary is exactly {"occurred_at"}, the canonical Asserted
// rendering is non-empty, and target-key extraction works (the Winner
// convention resolves against the right target chain).
func TestW4FifthCarrierRegistered(t *testing.T) {
	if !IsRegisteredCarrier(w4EvtCorrected) {
		t.Fatalf("W4-13a: IsRegisteredCarrier(%q) = false — occurred_at_corrected must join the §4.4 registry as the fifth carrier", w4EvtCorrected)
	}
	const correctedMS = w3Base - 9*w3Hour
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Registry Target")
	corr := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), correctedMS, "Riley Okpik")

	outs, err := w4OutcomeBudgeted(t, st, corr.Key())
	if err != nil {
		t.Fatalf("W4-13a: Outcome on a registered correction errored: %v", err)
	}
	var facts []string
	for _, o := range outs {
		facts = append(facts, o.Fact)
	}
	if !reflect.DeepEqual(facts, []string{w4FactOccurredAt}) {
		t.Fatalf("W4-13a: the correction carrier's fact vocabulary = %v, want exactly [%q] (VF §4.2's own vocabulary; corrections are the degenerate single-fact case)", facts, w4FactOccurredAt)
	}
	if strings.TrimSpace(outs[0].Asserted) == "" {
		t.Fatalf("W4-13a: the canonical Asserted rendering is empty — registration supplies it (VF §4.4's second good)")
	}
	if outs[0].Status != Effective || outs[0].Winner != corr.Key() {
		t.Fatalf("W4-13a: sole uncontested correction reports (%v, winner %v), want (Effective, itself) — target-key extraction resolved the wrong chain", outs[0].Status, outs[0].Winner)
	}
}

// TestW4UnregisteredCorrectionClassTypeErrors — W4-13(a)'s API half on the
// correction class: a wire event of an UNREGISTERED `_corrected` type is a
// non-nil error from Outcome — never ([]FactOutcome{}, nil).
func TestW4UnregisteredCorrectionClassTypeErrors(t *testing.T) {
	st := w3NewStore(t)
	w3RosterPair(t, st, "Unregistered X", "Unregistered Y")
	planted := w3WireAmendment(t, "site-b", 1, 1, w3Base, "person-nobody", nil)
	planted.Payload = []byte(`{"type":"` + w4PlantedCorrectionType + `","target_site_id":"site-a","target_node_epoch":1,"target_seq":1,"corrected_occurred_at":1,"recorded_by":"Nobody"}`)
	w3Apply(t, st, planted)
	outs, err := w4OutcomeBudgeted(t, st, planted.Key())
	if err == nil {
		t.Fatalf("W4-13a: Outcome on unregistered %q returned (%v, nil) — an empty success is the silent-OK shipped inside the mechanism built to kill it", w4PlantedCorrectionType, outs)
	}
}

// TestW4CarrierCoverageWithEmptyExemptions — W4-13(a)'s coverage half.
// CONTROLS FIRST, both driving w4CoverageProblems (the same judgement the
// clean pass runs): (1) the planted unregistered type is detected;
// (2) the planted bogus exemption row — naming a type absent from the
// enumeration source — is detected as a FAILURE, which makes the pre-W9
// dead-exemption case unreachable under W4's empty-list timing, and
// checkably so. Then the clean pass: a live log holding person_amended
// AND occurred_at_corrected, enumerated through W3-12's filter, empty
// exemption list, no problems.
func TestW4CarrierCoverageWithEmptyExemptions(t *testing.T) {
	// --- CONTROL 1: the planted unregistered type (CONTROL row 2) -------
	got := w4CoverageProblems([]string{w3EvtPersonAmended, w4PlantedCorrectionType}, nil, IsRegisteredCarrier)
	if len(got) != 1 || !strings.Contains(got[0], w4PlantedCorrectionType) {
		t.Fatalf("W4-13a CONTROL 1 FAILED: the coverage judgement did not flag the planted unregistered type %q (got %v) — a check that cannot detect its plant proves nothing", w4PlantedCorrectionType, got)
	}

	// --- CONTROL 2: the planted bogus exemption row (CONTROL row 6) -----
	// `alert_ack_retracted` is W9's future exemption row; at W4 it is
	// absent from every enumeration source, so a coverage test that
	// tolerates it as a dormant row has the silently-narrowing shape.
	got = w4CoverageProblems([]string{w3EvtPersonAmended}, []string{"alert_ack_retracted"}, func(string) bool { return true })
	if len(got) != 1 || !strings.Contains(got[0], "alert_ack_retracted") || !strings.Contains(got[0], "ABSENT") {
		t.Fatalf("W4-13a CONTROL 2 FAILED: a planted exemption row naming a type absent from the enumeration source was not reported as a coverage-test FAILURE (got %v) — never a skip, never a dormant row", got)
	}

	// --- the clean pass over a LIVE log ---------------------------------
	const correctedMS = w3Base - 9*w3Hour
	st := w3NewStore(t)
	x, _, _ := w3RosterPair(t, st, "Coverage W4 A", "Coverage W4 B")
	w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, Phone: w3Str("555-0771")})
	target := w4Target(t, st, "site-a", 1, "Coverage Target")
	w4CorrectLocal(t, st, "site-a", 1, w3Base+2*w3Hour, target.Key(), correctedMS, "Riley Okpik")

	inv := w3AmendmentFamilyTypes(t, st, [][2]interface{}{{"site-a", int64(1)}})
	want := []string{w4EvtCorrected, w3EvtPersonAmended}
	sort.Strings(want)
	if !reflect.DeepEqual(inv, want) {
		t.Fatalf("W4-13a: filtered inventory = %v, want exactly %v at W4 build time — an inventory missing this chunk's own carrier makes the coverage pass vacuous", inv, want)
	}
	// ⟦LOCK — W9 post-lock amendment §37.2, plan owner, 2026-09-03⟧ The clean
	// pass runs against the EMPTY exemption list it was locked with, restated
	// as a literal here rather than read from the shared package var.
	// ⟦was: "w4CoverageProblems(inv, w4ExemptNonCarriers, IsRegisteredCarrier)"⟧
	//
	// WHY, measured. W9b's SCHEDULED amendment (⟦B5 — W9R2-A-3⟧, above) grew
	// `w4ExemptNonCarriers` to hold `alert_ack_retracted` and
	// `arrival_ref_retracted`. Both end in `_retracted`; this fixture's
	// enumeration source is `w3AmendmentFamilyTypes`, which filters on
	// `_amended`/`_corrected`. So the rows can NEVER appear in THIS test's
	// inventory, and this test's own both-directions judgement then reports
	// each as "ABSENT from the enumeration source" — a red that does not heal
	// when W9c ships, because the clash is vocabulary, not timing.
	//
	// ⚠️ NOT A RELAXATION, and the alternative was measured before this was
	// chosen. Widening `w3AmendmentFamilyTypes` to `_retracted` is a NO-OP
	// here: this fixture's log holds no retraction event at all, so `inv` is
	// unchanged and the rows stay ABSENT; and seeding one instead reds line
	// 178's `reflect.DeepEqual(inv, want)` one assertion earlier. Either way
	// this test is red, and that helper has four call sites across three
	// locked test files.
	//
	// ⚠️ WHERE THE LIVE JUDGEMENT WENT — it did not go away. The dead-row
	// tooth is exercised in THREE places after this amendment:
	//   1. CONTROL 2 above, in this test, on this same `w4CoverageProblems`
	//      code path — a planted bogus exemption row must still be flagged.
	//   2. `TestW9b4RetractionIsEnumeratedAndExemptedWithAWrittenReason`
	//      (`w9b_carrier_coverage_test.go`) runs `w4CoverageProblems` over the
	//      WHOLE `w4ExemptNonCarriers` var against a live log that HOLDS both
	//      retraction types, under a classification stated as a suffix
	//      PROPERTY rather than a hand list — so a third dead row added later
	//      still reds there.
	//   3. That file's DEAD-ROW CONTROL requires both rows to be reported as
	//      failures against an inventory holding neither.
	// W4 keeps the coverage half it locked; W9b-4 holds the liveness half,
	// in the only file whose fixture can contain the vocabulary.
	if problems := w4CoverageProblems(inv, nil, IsRegisteredCarrier); len(problems) != 0 {
		t.Fatalf("W4-13a: coverage problems on the live log: %v — occurred_at_corrected must be registered with the three §4.4 goods, and the exemption list is EMPTY at W4 (this pass; the shared `w4ExemptNonCarriers` var carries W9's rows and is judged by W9b-4 — §37.2)", problems)
	}
}
