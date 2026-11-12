// w5b_created_half_test.go — W5b's CREATED HALF and the fold, in the domain:
//
//	W5b-0  PersonCreated carries Phone as nullable (*string, omitempty); the
//	       fold writes the `person.phone` column; a round trip returns the
//	       AUTHORED BYTES — no digit-stripping, no space/dash/paren removal,
//	       no country code prepended, no truncation, no case-fold
//	W5b-1  the D-52 half: gate add-on-demand creates a person with NO phone
//	       and SUCCEEDS — the assertion form of the ruling, not a sentence
//	W5b-3  a phoneless wire `person_created`, and a wire `person_amended`
//	       that never asserts phone, both fold CLEAN and compose phone absent
//	W5b-4  the WIRE half: a `person_amended` carrying Clear:["phone"] folds
//	       and composes phone as ABSENT — never refused, never stalled
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Seams and machinery: w5b_seams_test.go (same package).
//
// ⚠ D-52 GOVERNS THIS FILE. `person_created` is authored locally from TWO
// places — `web/intake.go:567` and `domain/checkin.go:291` (gate
// add-on-demand, which `refdata.go:323–326` calls "the creation site that
// matters most … how most persons enter the roster"). James ruled the gate
// door EXEMPT. So: **no test here asserts phone-mandatoriness as a property
// of `person_created`.** It is a property of ONE HANDLER, `handleIntake`,
// and it is asserted there (internal/web/w5b_intake_edge_test.go) and
// nowhere else. TestW5bGateAddOnDemandCreatesAPersonWithNoPhone below
// asserts the opposite — that the gate SUCCEEDS without a phone — because
// that is the ruling, not an omission.
package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

// =======================================================================
// W5b-0 — the created half lands, and it is assertable
// =======================================================================

// TestW5bPersonCreatedCarriesNullablePhone — W5b-0's payload half.
//
// EXPECTED RED (pre-chunk, genuine, unaided): `PersonCreated`
// (`refdata.go:37–44`) has six fields and no Phone [MEASURED at 88dd6a6].
// The census below reds naming the absent field.
//
// Kills (m5b-5)'s "`PersonCreated.Phone` absent" arm.
//
// The `omitempty` half is asserted TWICE and deliberately: once on the
// struct tag (the declaration) and once BEHAVIOURALLY (a phoneless
// PersonCreated must marshal without the key). A tag assertion alone passes
// on `json:"phone"` if the reader is sloppy; the behavioural half cannot.
// Absent and null are different things on the wire and W5b-3 turns on the
// difference.
func TestW5bPersonCreatedCarriesNullablePhone(t *testing.T) {
	present, typeName, tag := w5bPersonCreatedHasPhone()
	if !present {
		var have []string
		rt := reflect.TypeOf(PersonCreated{})
		for i := 0; i < rt.NumField(); i++ {
			have = append(have, rt.Field(i).Name)
		}
		t.Fatalf("W5b-0 (m5b-5): domain.PersonCreated has no %s field — it carries %v. W5b builds THREE things (the PersonCreated half, the person.phone column, the enrolment refusal); before rev 6 two of the three had no criterion that could fail on them",
			w5bGoFieldPhone, have)
	}
	if typeName != "*string" {
		t.Errorf("W5b-0 (SEAM W5b-S-CREATED): PersonCreated.%s is %s, want *string — \"nullable (*string, omitempty)\" is the criterion's own wording, and a pointer is what keeps ABSENT distinguishable from empty-string (W3-3)", w5bGoFieldPhone, typeName)
	}
	if want := w5bKeyPhone + ",omitempty"; tag != want {
		t.Errorf("W5b-0 (SEAM W5b-S-PAYLOAD/W5b-S-CREATED): PersonCreated.%s has json tag %q, want %q", w5bGoFieldPhone, tag, want)
	}

	// The behavioural half of omitempty: a phoneless PersonCreated must not
	// put the key on the wire at all.
	raw, err := json.Marshal(PersonCreated{Type: w5bEvtPersonCreated, PersonID: "site-a-person-1", Name: "Walkin, Wally"})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys[w5bKeyPhone]; ok {
		t.Errorf("W5b-0: a phoneless PersonCreated marshalled %s — `omitempty` is not in force, so \"no phone\" leaves this post as an explicit null. Absent and null are different facts and W5b-3 turns on the difference. Bytes: %s", w5bKeyPhone, raw)
	}

	// And the mirror: a phone-BEARING PersonCreated must put the key on the
	// wire under SEAM W5b-S-PAYLOAD's spelling. Without this arm the
	// assertion above is satisfied by a field that never marshals at all
	// (`json:"-"`), which is standing rule 5's shape.
	if typeName != "*string" {
		t.Fatalf("W5b-0: PersonCreated.%s is %s, so the marshalling arms below cannot run — fix the declared type first", w5bGoFieldPhone, typeName)
	}
	withPhone := reflect.New(reflect.TypeOf(PersonCreated{})).Elem()
	withPhone.FieldByName("Type").SetString(w5bEvtPersonCreated)
	withPhone.FieldByName("PersonID").SetString("site-a-person-2")
	withPhone.FieldByName("Name").SetString("Ferguson, Mary")
	const wire = "(907) 555-0134"
	withPhone.FieldByName(w5bGoFieldPhone).Set(reflect.ValueOf(w5bStr(wire)))
	raw, err = json.Marshal(withPhone.Interface())
	if err != nil {
		t.Fatal(err)
	}
	keys = nil
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	got, ok := keys[w5bKeyPhone]
	if !ok {
		t.Fatalf("W5b-0 (SEAM W5b-S-PAYLOAD): a PersonCreated carrying a phone marshalled WITHOUT the %q key: %s", w5bKeyPhone, raw)
	}
	var gotStr string
	if err := json.Unmarshal(got, &gotStr); err != nil {
		t.Fatalf("W5b-0: the %q key did not marshal as a JSON string: %s", w5bKeyPhone, got)
	}
	if gotStr != wire {
		t.Errorf("W5b-0 (m5b-6): the payload carried phone %q, want the authored bytes %q — phone is stored VERBATIM (plan `:2037`)", gotStr, wire)
	}
}

// TestW5bFoldWritesThePhoneToTheProjection — W5b-0's fold + round-trip half.
//
// EXPECTED RED (pre-chunk, genuine, unaided): the `person` projection is six
// columns (`refdata.go:197–204`, STRICT) with no `phone`, and
// `applyPersonCreated` (`:217–218`) inserts six values [MEASURED at
// 88dd6a6]. The raw row comes back with phone absent and this reds naming
// (m5b-5).
//
// Kills (m5b-5) — "`person.phone` dropped, renamed … or the fold silently
// not writing it".
//
// The read is the RAW projection row, not the composed person, and that is
// load-bearing: `Person.Phone` has existed since W3 and is populated by
// AMENDMENTS (`person_amend.go:362`). A composed read cannot tell "the fold
// wrote the created value" from "an amendment supplied it", so a composed
// read would go green over a fold that silently dropped the column — the
// exact silence (m5b-5) names.
func TestW5bFoldWritesThePhoneToTheProjection(t *testing.T) {
	st := w5bNewStore(t)
	const id = "site-b-person-w5b-fold"
	const phone = "(907) 555-0134"

	before := w5bPersonCount(t, st)
	w5bApply(t, st, w5bWirePersonCreated(t, "site-b", 1, 1, w5bBase, id, "Ferguson, Mary",
		map[string]any{w5bKeyPhone: phone}), "W5b-0")
	if after := w5bPersonCount(t, st); after != before+1 {
		t.Fatalf("fixture defect: the wire person_created added %d rows, want 1", after-before)
	}

	raw := w5bRawPersonRow(t, st, id)
	if raw.Phone == nil {
		t.Fatalf("W5b-0 (m5b-5): the RAW `person` projection row for %s carries no phone after a wire person_created that authored %q. Either the column does not exist or the fold does not write it — read the failure of the store-package introspection test beside this one to tell which", id, phone)
	}
	if *raw.Phone != phone {
		t.Errorf("W5b-0 (m5b-6): the raw row carries phone %q, want the authored bytes %q — verbatim means byte-for-byte (plan `:2037`)", *raw.Phone, phone)
	}

	// The composed read must agree: nothing amends this person, so
	// composition must carry the created value through unchanged.
	if p := w5bComposed(t, st, id); p.Phone == nil || *p.Phone != phone {
		t.Errorf("W5b-0: the COMPOSED person carries phone %s, want %q — with no amendments in the log, composition must pass the created value through", w5bShow(p.Phone), phone)
	}
}

// w5bByteFidelityValues are the shapes (m5b-6) names, one per named
// normalisation: parens+spaces+dash, an international prefix, an extension
// with letters, a very long number (truncation), and a lowercase/uppercase
// pair (case-fold). None carries LEADING OR TRAILING whitespace — see the
// seam note in the report: no document rules what an edge-trimmed phone
// should be, `composeName` collapses whitespace for `Name`, and inventing a
// rule here is not a test-writer's call.
var w5bByteFidelityValues = []string{
	"(907) 555-0134",
	"+1 907-555-0134",
	"01463 555 0134 ext 22",
	"+44 (0)7700 900 123 / 900 124",
	"0800 EXT-ninety-nine",
}

// TestW5bPhoneIsStoredByteFaithfully — W5b-0's byte-fidelity half, over both
// the CREATION path and the AMENDMENT path.
//
// The plan writes "`Phone` nullable, VERBATIM" at `:2037`. Revision 5 turned
// the cargo twin into a criterion and this one into nothing — on the field
// where normalisation is by far the likeliest builder reflex.
//
// EXPECTED RED — TWO SOURCES, and they are different in kind:
//   - the CREATION arm reds pre-chunk unaided (no column, no field);
//   - the AMENDMENT arm is (m5b-6)'s PLANT SITE and is producible TODAY: a
//     digit-normaliser inserted on the phone value in `applyPersonAmended`
//     (or at the `AmendPerson` edge) makes this arm red on the pre-chunk
//     tree. That is why the amendment path is asserted here at all —
//     W5b-0's own words are "at ANY authoring or fold path", and the
//     amendment path is the only phone-carrying path that exists before the
//     builder starts.
//
// Kills (m5b-6).
func TestW5bPhoneIsStoredByteFaithfully(t *testing.T) {
	st := w5bNewStore(t)

	// --- IN-TEST CONTROL, first, so the comparison is shown to bite.
	// A normaliser is applied to each fixture value and the comparison must
	// REJECT the result. Without this, "the stored value equals the authored
	// value" could be true of a comparator that normalises both sides — and
	// this criterion has no plantable mutant on the creation path before the
	// builder lands it, so the control is the tooth.
	for _, v := range w5bByteFidelityValues {
		norm := w5bStripToDigits(v)
		if norm == v {
			t.Fatalf("CONTROL FAILED: fixture value %q is unchanged by digit-stripping, so it cannot demonstrate (m5b-6) — every value in w5bByteFidelityValues must carry at least one non-digit a normaliser would remove", v)
		}
		if norm == "" {
			t.Fatalf("CONTROL FAILED: fixture value %q strips to nothing; the plant's output must still be a plausible phone or the comparison is trivial", v)
		}
	}

	// --- the AMENDMENT path. Exists at 88dd6a6, so this arm runs — and reds
	// — on the pre-chunk tree under (m5b-6)'s plant.
	base, err := CreatePerson(st, "site-a", 1, "Ferguson, Mary", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture defect: creating the amendment-path person: %v", err)
	}
	for i, v := range w5bByteFidelityValues {
		if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: base.PersonID, Phone: w5bStr(v)}, 0, "default"); err != nil {
			t.Fatalf("fixture defect: amending phone to %q: %v", v, err)
		}
		got := w5bComposed(t, st, base.PersonID).Phone
		if got == nil {
			t.Fatalf("W5b-0 (m5b-6), amendment path, value %d: phone composed ABSENT after an amendment asserting %q", i, v)
		}
		if *got != v {
			t.Errorf("W5b-0 (m5b-6), amendment path: phone %q was stored as %q — phone is byte-faithful: no digit-stripping, no space/dash/paren removal, no country code added, no truncation, no case-fold (plan `:2037`, \"verbatim\")", v, *got)
		}
	}

	// --- the CREATION path. Reds pre-chunk unaided.
	for i, v := range w5bByteFidelityValues {
		id := "site-b-person-w5b-bytes-" + string(rune('a'+i))
		w5bApply(t, st, w5bWirePersonCreated(t, "site-b", 1, int64(i+1), w5bBase, id, "Byte, Fidelity",
			map[string]any{w5bKeyPhone: v}), "W5b-0")
		raw := w5bRawPersonRow(t, st, id)
		if raw.Phone == nil {
			t.Fatalf("W5b-0 (m5b-5/m5b-6), creation path, value %d (%q): the raw `person` row carries no phone at all — the created half has not landed", i, v)
		}
		if *raw.Phone != v {
			t.Errorf("W5b-0 (m5b-6), creation path: phone %q was stored as %q — verbatim means byte-for-byte", v, *raw.Phone)
		}
	}
}

// w5bStripToDigits is the CONTROL's normaliser — the commonest shape of the
// mutant (m5b-6) names. It is never applied to anything the system stores;
// it exists only to prove the comparison above can tell a normalised value
// from an authored one.
func w5bStripToDigits(s string) string {
	out := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			out += string(r)
		}
	}
	return out
}

// =======================================================================
// W5b-1 — D-52's half: the gate's door is EXEMPT
// =======================================================================

// TestW5bGateAddOnDemandCreatesAPersonWithNoPhone — [[D-52]] made into an
// assertion rather than a sentence, and the second half of W5b-1.
//
// ⚠ THIS IS THE FALSE-GREEN GEOMETRY THE WHOLE SUB-CHUNK IS EXPOSED TO.
// `person_created` is authored from two places, both routed through
// `CreatePersonRecordedBy` → `createPerson` (`refdata.go:327`, `:331`). A
// phone check placed in that SHARED HELPER sits ABOVE the fold and BELOW the
// edge: it satisfies W5b-1's intake assertion, passes every intake-only
// test, and BREAKS THE BUSIEST OPERATIONAL PATH IN THE SYSTEM — a gate
// officer, mid-check-in, with a person standing in front of them.
//
// EXPECTED RED: not the clean pass — this passes on arrival, because no
// phone check exists anywhere yet. Its ledger evidence is the PLANTED
// (m5b-1) `createPerson` arm: a refusal placed in `createPerson` predicated
// on something the gate never supplies. Produced and recorded in
// reports/window-build/W5b-tests.md §3.
//
// Kills (m5b-1)'s second arm — "the check moved into
// `createPerson`/`CreatePersonRecordedBy`".
func TestW5bGateAddOnDemandCreatesAPersonWithNoPhone(t *testing.T) {
	st := w5bNewStore(t)
	before := w5bPersonCount(t, st)

	// Rule 6 — the arrangement this test needs, checked rather than assumed:
	// the slot must name someone NOT on the roster, or `createPerson` is
	// never reached and the test exercises nothing. The name carries no
	// comma and no company: `composeName`'s own doc says rows created at the
	// gate stay "First Last", and a walk-in with no company is the shape
	// that reaches createPerson with companyID nil.
	const walkIn = "Wanda Walkin"
	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.Name == walkIn {
			t.Fatalf("fixture defect: %q is already on the roster, so this is not the add-on-demand path and createPerson is never reached", walkIn)
		}
	}

	if _, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "gate walk-in"),
		Driver:    PersonSlot{NewName: walkIn},
	}, 0, "default"); err != nil {
		t.Fatalf("W5b-1 (m5b-1): gate add-on-demand REFUSED a person with no phone: %v.\n[[D-52]] rules the gate door EXEMPT — \"a gate officer need not obtain a phone number to add someone mid-check-in\". Phone is mandatory at INTAKE, the surface where the roster is built in advance, and NOT at the gate. A check in createPerson/CreatePersonRecordedBy sits above the fold and below the edge: it passes every intake-only test and breaks how most persons enter the roster", err)
	}

	if after := w5bPersonCount(t, st); after != before+1 {
		t.Fatalf("W5b-1: gate add-on-demand authored %d person rows, want 1", after-before)
	}
	all, err = Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var found *Person
	for i := range all {
		if all[i].Name == walkIn {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatalf("W5b-1: gate add-on-demand did not put %q on the roster", walkIn)
	}
	// And the gate must not INVENT one either: a fabricated placeholder on a
	// contact field is F20's "an honest absence beats a fabricated value"
	// applied to phone, and it is what "the muster will have gaps, and those
	// gaps cluster on people who arrived at a gate" (D-52) means in the data.
	if found.Phone != nil {
		t.Errorf("W5b-1 (D-52): gate add-on-demand invented phone %q for a walk-in nobody asked a number for. The accepted degradation is a GAP, recorded honestly — not a placeholder", *found.Phone)
	}
}

// =======================================================================
// W5b-3 — the column stays nullable: no poisoned-envelope stall
// =======================================================================

// TestW5bPhonelessWireEventsFoldCleanAndComposeAbsent — W5b-3.
//
// ⟦NO PRE-CHUNK RED⟧ and the lock calls it "the purest degenerate criterion
// in the document": on the pre-chunk tree a phoneless wire person folds
// clean BECAUSE THERE IS NO PHONE FIELD TO OMIT — the rule it certifies
// (nullable column, no stall) and the rival it excludes (no column at all)
// return the same answer on its own fixture.
//
// w5bRequireCreatedHalf makes that non-negotiable rather than advisory
// (standing rule 6): the fixture asserts that `PersonCreated.Phone` and
// `person.phone` EXIST before any conclusion is drawn, so the pre-W5b-0
// state produces a loud PRECONDITION RED instead of a green a gate could
// book. That red is the pre-chunk state, NOT this criterion's evidence.
//
// Kills (m5b-3) — "the fold refusing, erroring or stalling on a phoneless
// wire person" — RUN POST-W5b-0, per the lock.
func TestW5bPhonelessWireEventsFoldCleanAndComposeAbsent(t *testing.T) {
	st := w5bNewStore(t)
	w5bRequireCreatedHalf(t, st, "W5b-3")

	// (i) a wire `person_created` with NO phone key at all.
	const created = "site-c-person-w5b-nophone"
	w5bApply(t, st, w5bWirePersonCreated(t, "site-c", 1, 1, w5bBase, created, "Nophone, Nora", nil), "W5b-3")
	if raw := w5bRawPersonRow(t, st, created); raw.Phone != nil {
		t.Errorf("W5b-3: a wire person_created with no %q key landed a raw row carrying phone %q — absent must stay absent, never coerced to a value", w5bKeyPhone, *raw.Phone)
	}
	if p := w5bComposed(t, st, created); p.Phone != nil {
		t.Errorf("W5b-3: the composed person carries phone %s, want <absent> — nil means ABSENT and is a different thing from empty-string (W3-3)", w5bShow(p.Phone))
	}

	// (ii) a wire `person_amended` that never asserts phone. It must fold and
	// must not disturb what the creation left.
	w5bApply(t, st, w5bWirePersonAmended(t, "site-c", 1, 2, w5bBase, created,
		map[string]any{"role": "banksman"}, nil), "W5b-3")
	if p := w5bComposed(t, st, created); p.Phone != nil {
		t.Errorf("W5b-3: an amendment that never mentions phone left the composed phone as %s, want <absent> — a nil pointer means \"this amendment does not touch that field\"", w5bShow(p.Phone))
	}

	// (iii) the mirror, so (i) is not satisfied by a build that drops EVERY
	// phone: a wire person carrying a phone must still land it. Without this
	// arm, "phone composes absent" is true by construction of a broken fold,
	// which is standing rule 5's shape.
	const withPhone = "site-c-person-w5b-withphone"
	const value = "01463 555 0134"
	w5bApply(t, st, w5bWirePersonCreated(t, "site-c", 1, 3, w5bBase, withPhone, "Haphone, Hal",
		map[string]any{w5bKeyPhone: value}), "W5b-3")
	if p := w5bComposed(t, st, withPhone); p.Phone == nil || *p.Phone != value {
		t.Errorf("W5b-3 MIRROR: a wire person carrying phone %q composed %s — arm (i) would then be true of a fold that discards every phone, not of a nullable column", value, w5bShow(p.Phone))
	}
}

// =======================================================================
// W5b-4 — the WIRE half of the asymmetry
// =======================================================================

// TestW5bWireClearPhoneFoldsAndComposesAbsent — W5b-4's wire half.
//
// ⟦NO PRE-CHUNK RED — wire half only⟧: W3's `person_amendment.phone`
// machinery already folds this (`person_amend.go:145`, `:376–377`), so arm
// (a) below passes on arrival and its clean pass is NOT evidence. Its ledger
// evidence is (m5b-4), planted: a fold-time refusal of Clear:["phone"] in
// `applyPersonAmended`. Produced — see the report §3.
//
// THE ASYMMETRY IS THE POINT, and both halves are produced: the EDGE refuses
// (asserted at the intake edge — internal/web/w5b_intake_edge_test.go's
// TestW5bIntakeNeverEnrolsAPersonWithoutAPhone, which is where the edge half
// was RULED to live on 2026-08-31), while the WIRE fold accepts. Refusing at
// the fold would fork the two posts' logs permanently (W3-2) — a post that
// refuses what its peer authored never converges with it again.
//
// ⚠ The amendment edge is deliberately NOT the site of that refusal: see
// w5b_amendment_edge_test.go for the ruling and for the owed retrofit it
// leaves behind.
//
// Arm (b) is NEW at W5b and is guarded: a phone that arrived on the CREATED
// row, cleared by a later amendment, must compose absent. If the builder
// composes the base row's phone AFTER the amendments rather than before,
// (a) still passes and (b) does not.
func TestW5bWireClearPhoneFoldsAndComposesAbsent(t *testing.T) {
	st := w5bNewStore(t)

	// --- (a) phone arrives BY AMENDMENT, then a wire clear removes it.
	// Runs on the pre-chunk tree; needs nothing W5b builds.
	p, err := CreatePerson(st, "site-a", 1, "Clearme, Clara", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture defect: %v", err)
	}
	const amended = "07700 900 123"
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Phone: w5bStr(amended)}, 0, "default"); err != nil {
		t.Fatalf("fixture defect: setting the phone to clear: %v", err)
	}
	// Rule 6 — the arrangement: there must BE a phone to clear, or the
	// assertion below is true of a person who never had one and the fixture
	// stops posing its case.
	if got := w5bComposed(t, st, p.PersonID).Phone; got == nil || *got != amended {
		t.Fatalf("fixture defect: the person to be cleared composes phone %s, want %q — a clear that clears nothing proves nothing", w5bShow(got), amended)
	}
	w5bApply(t, st, w5bWirePersonAmended(t, "site-d", 1, 1, w5bBase, p.PersonID, nil, []string{w5bKeyPhone}), "W5b-4")
	if got := w5bComposed(t, st, p.PersonID).Phone; got != nil {
		t.Errorf("W5b-4(a) (m5b-4): after a wire Clear:[%q] the composed phone is %s, want <absent>. A cleared field composes as ABSENT, never as empty-string (W3-3)", w5bKeyPhone, w5bShow(got))
	}

	// --- (b) phone arrives ON THE CREATED ROW, then a wire clear removes it.
	// NEW at W5b; guarded, because before W5b-0 there is no created phone to
	// clear and this arm would certify nothing.
	w5bRequireCreatedHalf(t, st, "W5b-4(b)")
	const createdID = "site-d-person-w5b-clear"
	const createdPhone = "(907) 555-0199"
	w5bApply(t, st, w5bWirePersonCreated(t, "site-d", 1, w5bNextSeq(t, st, "site-d", 1), w5bBase,
		createdID, "Bornwith, Ben", map[string]any{w5bKeyPhone: createdPhone}), "W5b-4(b)")
	if got := w5bComposed(t, st, createdID).Phone; got == nil || *got != createdPhone {
		t.Fatalf("fixture defect: the created person composes phone %s, want %q — nothing to clear", w5bShow(got), createdPhone)
	}
	w5bApply(t, st, w5bWirePersonAmended(t, "site-d", 1, w5bNextSeq(t, st, "site-d", 1), w5bBase,
		createdID, nil, []string{w5bKeyPhone}), "W5b-4(b)")
	if got := w5bComposed(t, st, createdID).Phone; got != nil {
		t.Errorf("W5b-4(b) (m5b-4): a phone that arrived on the CREATED row survived a wire Clear:[%q] as %s. Composition folds amendments OVER the created state in ascending key order — a base row applied after the amendments un-does every clear", w5bKeyPhone, w5bShow(got))
	}
}
