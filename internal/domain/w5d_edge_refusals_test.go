// w5d_edge_refusals_test.go — W5d-22 (the company AMENDABLE SET and the
// company EDGE-REFUSAL SET, both enumerated as LITERALS: nine arms, each
// produced, each PRE-APPEND, each with its fold twin), W5d-13 (W3-18
// cloned INVERTED — company→person is refused at the authoring edge),
// W5d-17 (= W3-2's ride: edge refusals fire pre-append, the fold never
// refuses) and W5d-2's edge-control half (no `retired` on any authoring
// surface, D-41.1).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: W3-m6 (mint-then-refuse — an edge refusal AFTER Author
// has appended), W3-m24 (the kind check dropped — INVERTED here: a
// company→person same_as authors at the edge; reusing W3-m24's own
// person→company direction would re-prove W3-18 and leave W5d's kind check
// untested), (m2) (`Retired` resurrected on company — column, fact or edge
// control), (m10) (the FOLD refusing a wire Clear:["name"] or a cross-kind
// same_as — the log-forking shape), (m11) (an amendable-set member added or
// removed silently), (m11b) (an edge-refusal arm DROPPED from the company
// set — specifically a company same_as closing a CYCLE, authored at the
// edge and ACCEPTED; that arm is W5d-12's own stated premise, so without it
// the criterion two numbers down rests on a property nothing establishes).
//
// ⚠ W3-m2 is NOT ridden and is deliberately absent: its registered shape is
// "refuse unknown trade_id in the fold", and company's amendable set has no
// trade_id limb, so the kill cannot be produced. That is the same
// measurement that makes person arm 3 have no company instance below.
package domain

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- W5d-22(a): the amendable set, as literals --------------------------

// w5dForbiddenFactProblems reports every fact in the candidate set that has
// NO company limb — the detector shared by the CONTROL plant and the real
// assertion (one code path, so a control red proves the path the clean pass
// runs).
func w5dForbiddenFactProblems(facts []string) []string {
	forbidden := map[string]bool{}
	for _, f := range w5dNonCompanyFacts {
		forbidden[f] = true
	}
	var problems []string
	for _, f := range facts {
		if forbidden[f] {
			problems = append(problems, "fact "+f+" has NO company limb — the company amendable set is {name, same_as} and nothing else (W5d-22(a); `retired` additionally by D-41.1 / W5d-2)")
		}
	}
	sort.Strings(problems)
	return problems
}

// w5dWireFactsOf returns the JSON key spellings a marshalled payload struct
// declares, in field order, excluding the type discriminator.
func w5dWireFactsOf(v any) []string {
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" || name == "type" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// TestW5dAmendableSetIsExactlyNameAndSameAs — W5d-22(a) and W5d-2's fact
// half. The amendable set is {name, same_as} AS LITERALS: asserted on the
// wire payload's declared keys, on the authoring-edge input's declared
// fields, and BEHAVIOURALLY at the edge — every fact with no company limb
// is refused through the `Clear` vocabulary. Kills (m11) and (m2).
func TestW5dAmendableSetIsExactlyNameAndSameAs(t *testing.T) {
	// --- CONTROL FIRST: a planted amendable set carrying `retired` (and a
	// second planted limb) must be flagged by the same detector. ---------
	planted := w5dForbiddenFactProblems([]string{"name", "same_as", "retired", "trade_id"})
	if len(planted) != 2 {
		t.Fatalf("W5d-2/W5d-22(a) CONTROL FAILED: the forbidden-fact detector reported %d problem(s) on a planted set carrying `retired` and `trade_id` (%v), want 2 — a detector that cannot detect its plant proves nothing", len(planted), planted)
	}
	if !strings.Contains(strings.Join(planted, " "), "retired") {
		t.Fatalf("W5d-2 CONTROL FAILED: the planted `retired` fact was not named in %v", planted)
	}
	if clean := w5dForbiddenFactProblems(w5dAmendableFacts); len(clean) != 0 {
		t.Fatalf("W5d-22(a) CONTROL: the detector flagged the criterion's own literal set %v: %v", w5dAmendableFacts, clean)
	}

	// --- the wire payload's declared keys -------------------------------
	wireKeys := w5dWireFactsOf(CompanyAmended{})
	// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` joins the wire keys ([[D-92]],
	// step 3): the causal reference, stamped by the edge and never a fact an
	// officer asserts — so the amendable SET below is still {name, same_as}
	// and the input census is untouched. ⟦was: {"company_id", "name",
	// "same_as", "clear"}⟧
	wantWire := []string{"company_id", "name", "same_as", "clear", "seen"}
	if !reflect.DeepEqual(wireKeys, wantWire) {
		t.Fatalf("W5d-22(a)/W5d-2b: CompanyAmended's wire keys are %v, want EXACTLY %v (B-2's freeze list for item 12: the event-type name company_amended, the field-name literals company_id/name/same_as, the lowercase `clear` key, and — since SchemaVersion 11 — `seen` (D-92) — recorded_by was STRUCK from that list at rev 4 and must not appear)", wireKeys, wantWire)
	}
	if p := w5dForbiddenFactProblems(wireKeys); len(p) != 0 {
		t.Fatalf("W5d-22(a) (m11): %v", p)
	}

	// --- the authoring edge's declared input ----------------------------
	inputFields := w5dExportedFieldCensus(CompanyAmendment{})
	wantInput := []string{
		"CompanyID string",
		"Name *string",
		"SameAs *string",
		"Clear []string",
	}
	if !reflect.DeepEqual(inputFields, wantInput) {
		t.Fatalf("W5d-22(a) (m11): CompanyAmendment's exported fields are %v, want EXACTLY %v — an amendable-set member added or removed silently is the mutant this literal enumeration exists to catch. `Retired` in particular is refused by D-41.1 (\"A company can be corrected but never retired\") and CompanyCreated's own RecordedBy field is the tempting precedent pointing the WRONG way", inputFields, wantInput)
	}

	// --- BEHAVIOURALLY, at the edge: every non-company fact is refused
	// through the Clear vocabulary (the ONLY way an out-of-set fact is
	// representable — setFields() builds its map from the struct's own
	// fields, so a Set-side assertion of an out-of-set fact cannot even be
	// constructed). ------------------------------------------------------
	st := w3NewStore(t)
	x, _, _ := w5dCompanyPair(t, st, "Amendable X Ltd", "Amendable Y Ltd")
	for _, fact := range w5dNonCompanyFacts {
		w5dAssertRefusedPreAppend(t, st, "W5d-22(a)/W5d-2 (m11, m2): Clear:["+fact+"]",
			CompanyAmendment{CompanyID: x, Clear: []string{fact}})
	}
	// LIVENESS: an in-set Clear must be ACCEPTED, or "everything is
	// refused" would satisfy the loop above vacuously.
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
}

// --- W5d-22(b): the NINE edge-refusal arms, each PRE-APPEND -------------

// w5dAssertRefusedPreAppend asserts one edge refusal AND its pre-append
// property: the refusal returns a non-nil error, the stream watermark is
// unchanged, and no new company_amended envelope exists. Kills W3-m6
// (mint-then-refuse — the event minted, THEN refused).
func w5dAssertRefusedPreAppend(t *testing.T, st *store.Store, label string, in CompanyAmendment) {
	t.Helper()
	before := w3Watermark(t, st, "site-a", 1)
	env, err := AmendCompany(st, "site-a", 1, in, 0, "default")
	if err == nil {
		t.Fatalf("%s: the edge ACCEPTED the amendment (minted %v) — this arm of the company edge-refusal set is MISSING. The arm's own named mutant is in the label above: (m11b) for the CYCLE arm (W5d-12's stated premise), W3-m24 for the KIND arm (W5d-13, INVERTED), (m11)/(m2) for an amendable-set change", label, env.Key())
	}
	after := w3Watermark(t, st, "site-a", 1)
	if after != before {
		t.Fatalf("%s (W3-m6): the amendment was refused BUT THE LOG GREW (watermark %d → %d) — every edge refusal is PRE-APPEND: a refusal returns before Author is called, so nothing exists in the log. Error was: %v", label, before, after, err)
	}
	for _, e := range w3EventsAfter(t, st, "site-a", 1, before) {
		t.Fatalf("%s (W3-m6): a refused amendment left envelope %v in the log", label, e.Key())
	}
}

// TestW5dCompanyEdgeRefusalSetNineArms — W5d-22(b): the company
// edge-refusal set is NINE arms, enumerated against AmendPerson's TEN
// (`internal/domain/person_amend.go:533–644`, `grep -c 'refused —'` → 10
// [MEASURED at the lock]), each PRODUCED here. Person arm 3 (unknown
// trade_id) has NO company instance: company's amendable set is
// {name, same_as}, there is no trade_id limb and no trade register — which
// is the same measurement that strikes W3-m2 from W5d-17's kills.
func TestW5dCompanyEdgeRefusalSetNineArms(t *testing.T) {
	st := w3NewStore(t)
	a, b, _ := w5dCompanyPair(t, st, "Refusal A Ltd", "Refusal B Ltd")
	c, err := CreateCompany(st, "site-a", 1, "Refusal C Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	person, err := CreatePerson(st, "site-a", 1, "Not A Company", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// Arms 1, 2, 4, 5, 6, 7, 8 need no standing state.
	arms := []struct {
		n     int
		label string
		in    CompanyAmendment
	}{
		{1, "the amendment changes nothing (no field set, nothing cleared) [person_amend.go:556]",
			CompanyAmendment{CompanyID: a}},
		{2, "`name` amended to empty — company's ONLY content refusal, where person has two [:561]",
			CompanyAmendment{CompanyID: a, Name: w3Str("   ")}},
		{4, "Clear:[\"name\"] — a company always has a name, mirroring the person rule [:573]",
			CompanyAmendment{CompanyID: a, Clear: []string{w5dFactName}}},
		{5, "a Clear naming a fact OUTSIDE the amendable set [:576] — reachable through the Clear vocabulary ONLY",
			CompanyAmendment{CompanyID: a, Clear: []string{"phone"}}},
		{6, "the same fact both SET and CLEARED in one amendment [:579]",
			CompanyAmendment{CompanyID: a, SameAs: w3Str(b), Clear: []string{w5dFactSameAs}}},
		{7, "a record merged with ITSELF (the trivial self-loop) [:587]",
			CompanyAmendment{CompanyID: a, SameAs: w3Str(a)}},
		// ⚠ Arm 8 amends company C, not A: if the kind check is DROPPED the
		// edge ACCEPTS, and an accepted company→person merge on A would
		// leave A merged, cascading into arms 9 and 10 as a fixture failure
		// instead of a clean kill. Isolating the target keeps W3-m24's
		// inverted kill attributable to its own arm.
		{8, "same_as target is not of the right kind — INVERTED: a NON-COMPANY (person) target [:597]",
			CompanyAmendment{CompanyID: c.CompanyID, SameAs: w3Str(person.PersonID)}},
	}
	for _, arm := range arms {
		t.Run("arm "+string(rune('0'+arm.n))+" — "+arm.label, func(t *testing.T) {
			w5dAssertRefusedPreAppend(t, st, "W5d-22(b) arm "+arm.label, arm.in)
		})
	}

	// Arm 9 — a STANDING PRIOR MERGE ("clear same_as (the un-merge)
	// first"). Needs a standing merge, so it is authored first.
	w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: a, SameAs: w3Str(b)})
	if got := w5dCompany(t, st, a); got.SameAs == nil || *got.SameAs != b {
		t.Fatalf("W5d-22(b) arm 9 fixture defect: the standing merge did not compose (same_as=%s)", w5dSameAsOf(got))
	}
	t.Run("arm 9 — a standing prior merge [:604]", func(t *testing.T) {
		w5dAssertRefusedPreAppend(t, st, "W5d-22(b) arm 9 (standing prior merge)",
			CompanyAmendment{CompanyID: a, SameAs: w3Str(c.CompanyID)})
	})

	// Arm 10 — the CYCLE. This is W5d-12's OWN STATED PREMISE: W3-17's
	// fixture authors on primed scratch edges precisely because "edges
	// refuse cycles locally; folds never refuse — the cycle forms only on
	// the wire". If the company edge does not refuse cycles, that premise
	// is FALSE for company and W5d-12's fixture construction is
	// unmotivated. Arm 10 is also LoadCompanyMergeMap's SECOND consumer:
	// the check walks the composed resolution state, never a private
	// snapshot.
	t.Run("arm 10 — merging would close a CYCLE [:620]", func(t *testing.T) {
		// Fixture precondition (rule 6): a→b must stand, so b→a closes one.
		if got := w5dMergeMap(t, st).Resolve(a); got != b {
			t.Fatalf("W5d-22(b) arm 10 fixture defect: Resolve(%s) = %s, want the standing %s — no cycle can be closed", a, got, b)
		}
		w5dAssertRefusedPreAppend(t, st, "W5d-22(b) arm 10 (m11b — the arm rev 6 omitted, and W5d-12's premise)",
			CompanyAmendment{CompanyID: b, SameAs: w3Str(a)})
	})

	// Two further guards, named so the count of NINE stays checkable: the
	// person side's `person_id` required (:535) and "%q is not on the
	// roster" (:542) carry no `refused —` spelling — they are
	// input-validity, not fact-domain, refusals. The company edge carries
	// their analogues; they are NOT among the nine.
	t.Run("not-an-arm: company_id required", func(t *testing.T) {
		w5dAssertRefusedPreAppend(t, st, "W5d-22(b) input validity (company_id required, NOT one of the nine)",
			CompanyAmendment{CompanyID: "", Name: w3Str("Anything Ltd")})
	})
	t.Run("not-an-arm: unknown company", func(t *testing.T) {
		w5dAssertRefusedPreAppend(t, st, "W5d-22(b) input validity (unknown company, NOT one of the nine)",
			CompanyAmendment{CompanyID: "company-does-not-exist", Name: w3Str("Anything Ltd")})
	})
}

// TestW5dKindCheckRefusesACompanyToPersonMerge — W5d-13 (W3-18 cloned
// against company, INVERTED). W3-m24's registered fixture is a
// person→company same_as — the PERSON-side direction. W5d's fixture must
// author COMPANY→PERSON; reusing W3-m24's direction re-proves W3-18 and
// leaves W5d's kind check untested. Refusal is PRE-APPEND per W5d-17.
//
// This criterion catches the EDGE half only; the FOLD half is W5d-22(c)'s
// (a cross-kind same_as arriving by wire FOLDS — refusing it would fork the
// two posts' logs permanently).
func TestW5dKindCheckRefusesACompanyToPersonMerge(t *testing.T) {
	st := w3NewStore(t)
	company, _, _ := w5dCompanyPair(t, st, "Kind Check Ltd", "Kind Other Ltd")
	person, err := CreatePerson(st, "site-a", 1, "Kind Check Person", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Fixture precondition (rule 6): the target must be a REAL person and
	// NOT a company, or the refusal below could be the unknown-target one.
	if p, err := PersonByID(st, person.PersonID); err != nil || p == nil {
		t.Fatalf("W5d-13 fixture defect: the person target %s does not exist (err=%v)", person.PersonID, err)
	}
	if c, err := CompanyByID(st, person.PersonID); err != nil || c != nil {
		t.Fatalf("W5d-13 fixture defect: the person id %s also resolves as a COMPANY — the kind check cannot discriminate", person.PersonID)
	}
	w5dAssertRefusedPreAppend(t, st,
		"W5d-13 (W3-m24, INVERTED — company→person): company_amended.same_as naming a NON-COMPANY target",
		CompanyAmendment{CompanyID: company, SameAs: w3Str(person.PersonID)})
}

// --- W5d-22(c) and W5d-17: the fold NEVER refuses -----------------------

// TestW5dWireEquivalentsOfEveryRefusalFold — W5d-22(c) and W5d-17
// (= W3-2's ride): the fold half of EVERY edge-refusal arm is re-produced.
// A wire-received amendment passed its authoring post's edge (or was minted
// by a divergent one) and refusing it here would fork the two logs forever.
// Kills (m10) — the fold refusing a wire Clear:["name"] or a cross-kind
// same_as, the log-forking shape.
func TestW5dWireEquivalentsOfEveryRefusalFold(t *testing.T) {
	st := w3NewStore(t)
	a, b, _ := w5dCompanyPair(t, st, "Fold A Ltd", "Fold B Ltd")
	person, err := CreatePerson(st, "site-a", 1, "Fold Person", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	folds := []struct {
		arm    string
		fields map[string]any
		target string
	}{
		{"arm 1 — the wire no-op (nothing set, nothing cleared)", map[string]any{}, a},
		{"arm 2 — name asserted EMPTY", map[string]any{"name": ""}, a},
		{"arm 4 — Clear:[\"name\"] FROM THE WIRE (W5d-22(d)'s poisoned envelope)", map[string]any{"clear": []string{"name"}}, a},
		{"arm 5 — Clear naming a fact outside the amendable set", map[string]any{"clear": []string{"retired"}}, a},
		{"arm 6 — the same fact set AND cleared", map[string]any{"same_as": b, "clear": []string{"same_as"}}, a},
		{"arm 7 — a self-merge", map[string]any{"same_as": a}, a},
		{"arm 8 — a CROSS-KIND same_as: company → PERSON (the INVERTED direction; W3-18's landed fold shape is a company target for a person)", map[string]any{"same_as": person.PersonID}, a},
		{"arm 9 — a second, contradictory standing merge", map[string]any{"same_as": b}, a},
		{"arm 10 — a CYCLE-forming assertion (the cycle forms only on the wire)", map[string]any{"same_as": a}, b},
	}
	seq := int64(0)
	for _, f := range folds {
		seq++
		env := w5dWireAmendment(t, "site-b", 1, seq, w3Base+seq*w3Hour, f.target, f.fields)
		applied, err := st.Apply(env)
		if err != nil {
			t.Fatalf("W5d-22(c)/W5d-17 (m10): the FOLD REFUSED %s: %v — folds never refuse; refusing at fold time forks the two posts' logs permanently. Edge validation happened (or failed to happen) at the AUTHORING post, pre-append", f.arm, err)
		}
		if !applied {
			t.Fatalf("W5d-22(c): %s deduped unexpectedly", f.arm)
		}
	}

	// The composed state after all of it must still be readable and stable
	// — a fold that lands unrepresentable data must not make the readers
	// throw (the demoted-clause discipline: fold-accepts, no composed-value
	// pin beyond what W5d-22(d) pins).
	for i := 0; i < 3; i++ {
		w5dCompanies(t, st)
		m := w5dMergeMap(t, st)
		w3BudgetedResolve(t, m, a)
		w3BudgetedResolve(t, m, b)
	}
}

// TestW5dWireClearNameComposesAndSurfaces — W5d-22(d): `company.name` is
// TEXT NOT NULL and CompanyCreated.Name is a bare string [MEASURED —
// refdata.go:109, :25]; the edge refuses name-to-empty and Clear:["name"];
// the fold never refuses; and a cleared fact composes as ABSENT. So a
// wire-received company_amended carrying Clear:["name"] composes a company
// with NO NAME — and that composition is PINNED here rather than
// discovered. Its rendered consequence (the muster's third preference arm,
// the raw company id, during a roll call) is pinned at
// TestW5dLiveMusterPreferenceRuleAllThreeArms.
func TestW5dWireClearNameComposesAndSurfaces(t *testing.T) {
	st := w3NewStore(t)
	c, err := CreateCompany(st, "site-a", 1, "Poisoned Envelope Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Fixture precondition (rule 6): the EDGE must refuse this, or the wire
	// path is not the only way the case arises and the criterion is moot.
	w5dAssertRefusedPreAppend(t, st, "W5d-22(d) precondition: the edge refuses Clear:[\"name\"]",
		CompanyAmendment{CompanyID: c.CompanyID, Clear: []string{w5dFactName}})

	w3Apply(t, st, w5dWireAmendment(t, "site-b", 1, 1, w3Base+1*w3Hour, c.CompanyID,
		map[string]any{"clear": []string{w5dFactName}}))

	got, present := w5dCompanyName(w5dCompany(t, st, c.CompanyID))
	if present {
		t.Fatalf("W5d-22(d): after a wire Clear:[\"name\"] the composed company name is %q, want ABSENT — the fold never refuses and W5d-18 says a cleared fact composes as absent, so this composition is the chosen behaviour, pinned here", got)
	}
	// The company must still be LISTED — a nameless company is a poisoned
	// envelope, never a stalled projection.
	found := false
	for _, q := range w5dCompanies(t, st) {
		if q.CompanyID == c.CompanyID {
			found = true
		}
	}
	if !found {
		t.Fatalf("W5d-22(d): the nameless company vanished from Companies() — a poisoned envelope must compose, never stall the projection")
	}
}
