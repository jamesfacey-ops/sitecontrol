// w5b_amendment_edge_test.go — W5b does NOT retrofit the person AMENDMENT
// edge, and that is a RULING made checkable rather than a gap left open.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Seams and machinery: w5b_seams_test.go (same package).
//
// ⚠ WHAT THIS FILE USED TO CONTAIN, AND WHY IT NO LONGER DOES.
// W5b-4's body says "`handleIntake` refuses the clear (phone is mandatory
// there)". `handleIntake` HAS NO CLEAR OPERATION [MEASURED at 88dd6a6]: it
// is a creation handler over six text/select controls (`intake.go:399–577`),
// and `Clear` is a field of `PersonAmendment` (`person_amend.go:105`) and
// `PersonAmended` (`:70`) only. The edge/fold pairing the criterion
// describes — and the pairing (m5b-4) names, "the edge refusal applied to
// the WIRE path too (FOLD-TIME refusal of Clear:[\"phone\"])" — is
// `AmendPerson`/`applyPersonAmended`, stated in terms at
// `person_amend.go:17–19`.
//
// This writer produced BOTH readings and referred the choice to the gate.
// **RULED 2026-08-31: the literal `handleIntake` reading stands; the
// amendment-edge reading is STRUCK.** The ground is W5-X2's own precedent —
// *"W5d does NOT retrofit the person amendment form — that is a change to
// shipped behaviour outside W5d's scope"* — one sub-chunk over: adding a
// refusal to W3's shipped `AmendPerson` edge is the same move, and if W5d may
// not make it W5b may not either. W5b-4's edge half therefore rests on
// `TestW5bIntakeNeverEnrolsAPersonWithoutAPhone`
// (internal/web/w5b_intake_edge_test.go), and the deleted test was
// ⟦was: "TestW5bAmendmentEdgeRefusesClearingPhone"⟧.
//
// ⚠ THE CONSEQUENCE, RECORDED RATHER THAN SMOOTHED OVER. Because the shipped
// amendment edge accepts `Clear:["phone"]`, **phone is clearable after
// enrolment**. With [[D-52]] (gate add-on-demand exempt), the honest system
// property is: phone is mandatory at INTAKE ONLY, not at the gate, and
// REVOCABLE AFTERWARDS. The retrofit is **OWED AND UNOWNED** — a candidate
// for the owed register, not a thing that quietly evaporates.
//
// ⚠ WHAT SURVIVES HERE IS THE RULING ITSELF, MADE FAILABLE, AND IT IS NOT A
// W5b CRITERION. A builder reading W5b-4's body literally will reach for
// `AmendPerson`, because that is the only place the mechanism it names
// exists. This test reds if they do. It asserts the DECISION — no retrofit —
// so that the decision is checkable in the same way the criteria are.
// **Whoever lands the owed retrofit strikes this test in the same commit**;
// its red at that moment is the register working, not a regression.
package domain

import "testing"

// TestW5bDoesNotRetrofitTheAmendmentEdge — the ruling, made checkable.
//
// ⟦NO PRE-CHUNK RED — it passes on arrival, deliberately.⟧ It is not a
// criterion and carries no ledger row; it is a guard against a build that
// widens past its scope, and its evidence is that it FAILS on that build.
// Produced: with a `Clear:["phone"]` refusal planted at `AmendPerson` — the
// struck reading, implemented — this test goes RED (see §6.1 of
// reports/window-build/W5b-tests.md).
func TestW5bDoesNotRetrofitTheAmendmentEdge(t *testing.T) {
	st := w5bNewStore(t)
	p, err := CreatePerson(st, "site-a", 1, "Amendable, Amy", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture defect: %v", err)
	}

	// (i) a phone can still be SET by amendment. A build that cannot record a
	// phone number on an existing person has not built item 2, and this arm
	// is what stops "the amendment edge is unchanged" being satisfied by an
	// edge that refuses everything.
	const phone = "01463 555 0134"
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Phone: w5bStr(phone)}, 0, "default"); err != nil {
		t.Fatalf("W5b scope guard: the amendment edge refused to SET a phone: %v", err)
	}
	if got := w5bComposed(t, st, p.PersonID).Phone; got == nil || *got != phone {
		t.Fatalf("fixture defect: setting the phone composed %s, want %q — the clear below would then clear nothing", w5bShow(got), phone)
	}

	// (ii) `Clear:["phone"]` is STILL ACCEPTED at the amendment edge, and
	// composes the phone ABSENT (W3-3: absent, never empty-string).
	//
	// This is the shipped W3 behaviour that W5b deliberately does NOT change.
	// ⚠ It is ALSO the honest record of what "enrolment-mandatory" now buys:
	// intake insists on a phone, the gate does not (D-52), and an amendment
	// can take it away again afterwards.
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Clear: []string{w5bKeyPhone}}, 0, "default"); err != nil {
		t.Errorf("W5b SCOPE GUARD: the amendment edge REFUSED Clear:[%q]: %v\n"+
			"W5b-4's body names `handleIntake` as refusing the clear, and `handleIntake` has no clear operation — the only place that mechanism exists is HERE, so a builder following the criterion body literally lands the refusal at this edge.\n"+
			"RULED 2026-08-31: that is OUT OF W5b's SCOPE, on W5-X2's own precedent that a sub-chunk does not retrofit the shipped person amendment surface. The retrofit is OWED AND UNOWNED.\n"+
			"If you are the chunk that OWNS it, strike this test in the same commit — this red is the owed register working, not a regression.", w5bKeyPhone, err)
	}
	if got := w5bComposed(t, st, p.PersonID).Phone; got != nil {
		t.Errorf("W5b scope guard: after Clear:[%q] the composed phone is %s, want <absent> — a cleared fact composes ABSENT, never empty-string (W3-3)", w5bKeyPhone, w5bShow(got))
	}

	// (iii) and the rest of the clear mechanism is untouched — W5b narrows no
	// fact's clear at this edge, so another fact must still clear.
	const role = "banksman"
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Role: w5bStr(role)}, 0, "default"); err != nil {
		t.Fatalf("fixture defect: setting role: %v", err)
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Clear: []string{"role"}}, 0, "default"); err != nil {
		t.Errorf("W5b scope guard: the amendment edge refused Clear:[\"role\"]: %v — W5b touches no fact's clear at this edge", err)
	}
	if got := w5bComposed(t, st, p.PersonID).Role; got != nil {
		t.Errorf("W5b scope guard: Clear:[\"role\"] left role as %s, want <absent>", w5bShow(got))
	}
}
