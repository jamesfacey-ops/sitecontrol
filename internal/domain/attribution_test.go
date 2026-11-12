// attribution_test.go — F2's own done-when tests (test-campaign fix,
// promoted from the auth angle's out-of-angle note): company/workgroup/
// person creation, PLUS ref_alias creation, now carry recorded_by the same
// way visit/trip/arrival/cancel/check-out/ack already do. A NEW file on
// purpose — TEST_CAMPAIGN.md's fix brief is explicit that no existing test
// may be modified, so every case here is additive, exercising the new
// CreateCompanyRecordedBy/CreateWorkgroupRecordedBy/CreatePersonRecordedBy/
// CreateAliasRecordedBy siblings and (the site that actually matters)
// GateCheckIn's add-on-demand path, never touching the pre-existing
// CreateCompany/CreateWorkgroup/CreatePerson/CreateAlias call sites that
// every other test file already depends on.
package domain

// ⟦LOCK — §60, 2026-09-03⟧ Import follow-on: `encoding/json` and `internal/store` were
// used ONLY by ⟦was: "TestRefAliasRecordedByTiebreakIsOrderIndependent"⟧, retired above. They are
// dropped because Go faults an unused import; the retirement note preserves every line
// that used them.
// ⟦was: "\t\"encoding/json\""⟧
// ⟦was: "\t\"sitecontrol/internal/store\""⟧
import (
	"testing"
)

func postPtr(name string) *string { return &name }

// --- direct attribution on the three reference-data creation functions ---

func TestCreateCompanyRecordedByAttributesThePost(t *testing.T) {
	st := newDomainTestStore(t)
	c, err := CreateCompanyRecordedBy(st, "site-a", 1, "Kakivik", postPtr("Ice Road"), 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if c.RecordedBy == nil || *c.RecordedBy != "Ice Road" {
		t.Fatalf("company.RecordedBy = %v, want \"Ice Road\"", c.RecordedBy)
	}
	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 1 || companies[0].RecordedBy == nil || *companies[0].RecordedBy != "Ice Road" {
		t.Fatalf("stored company recorded_by = %+v, want \"Ice Road\"", companies)
	}
}

func TestCreateTradeRecordedByAttributesThePost(t *testing.T) {
	st := newDomainTestStore(t)
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 follow-on, UNMASKED at §60: with the
	// company argument dropped above, `c` became unused. ⚠ The compiler could not report this while
	// the package had earlier errors — the same masking this pass documented for the Workgroup class,
	// here applied to its OWN repair. The creation is KEPT (it still exercises the authoring path and
	// shapes the stream); only the now-vestigial binding is dropped.
	// ⟦was: "c, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default")"⟧
	if _, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "w, err := CreateTradeRecordedBy(st, "site-a", 1, c.CompanyID, "Roughnecks", postPtr("Ice Road"), 0, "default")"⟧
	w, err := CreateTradeRecordedBy(st, "site-a", 1, "Roughnecks", postPtr("Ice Road"), 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if w.RecordedBy == nil || *w.RecordedBy != "Ice Road" {
		t.Fatalf("workgroup.RecordedBy = %v, want \"Ice Road\"", w.RecordedBy)
	}
	wgs, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(wgs) != 1 || wgs[0].RecordedBy == nil || *wgs[0].RecordedBy != "Ice Road" {
		t.Fatalf("stored workgroup recorded_by = %+v, want \"Ice Road\"", wgs)
	}
}

func TestCreatePersonRecordedByAttributesThePost(t *testing.T) {
	st := newDomainTestStore(t)
	p, err := CreatePersonRecordedBy(st, "site-a", 1, "Alice Anderson", nil, nil, postPtr("Ice Road"), 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if p.RecordedBy == nil || *p.RecordedBy != "Ice Road" {
		t.Fatalf("person.RecordedBy = %v, want \"Ice Road\"", p.RecordedBy)
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 || persons[0].RecordedBy == nil || *persons[0].RecordedBy != "Ice Road" {
		t.Fatalf("stored person recorded_by = %+v, want \"Ice Road\"", persons)
	}
}

// --- the nil contract: NEVER a fabricated placeholder (mirrors F20/M11's
// own guard for visit.recorded_by — TestPostAttributionFallsBackToSiteIDRatherThanBlank
// in internal/web/auth_test.go) ---

func TestReferenceDataRecordedByIsNilWhenNotAttributed(t *testing.T) {
	st := newDomainTestStore(t)

	// The plain (unattributed) functions: unchanged behaviour, still NULL.
	c, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if c.RecordedBy != nil {
		t.Fatalf("CreateCompany recorded_by = %q, want nil", *c.RecordedBy)
	}

	// The attributed functions, called with a nil pointer (nothing to
	// yet) — must store a genuine SQL NULL, not "" or a placeholder string.
	c2, err := CreateCompanyRecordedBy(st, "site-a", 1, "Beta Co", nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if c2.RecordedBy != nil {
		t.Fatalf("CreateCompanyRecordedBy(nil) recorded_by = %q, want nil", *c2.RecordedBy)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "w, err := CreateTradeRecordedBy(st, "site-a", 1, c2.CompanyID, "Crew", nil, 0, "default")"⟧
	w, err := CreateTradeRecordedBy(st, "site-a", 1, "Crew", nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if w.RecordedBy != nil {
		t.Fatalf("CreateWorkgroupRecordedBy(nil) recorded_by = %q, want nil", *w.RecordedBy)
	}
	p, err := CreatePersonRecordedBy(st, "site-a", 1, "No Officer Yet", nil, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if p.RecordedBy != nil {
		t.Fatalf("CreatePersonRecordedBy(nil) recorded_by = %q, want nil", *p.RecordedBy)
	}

	// Re-read every row from the projection (not just the return value) —
	// the return value is constructed in Go before authoring; the row is
	// what a fold actually persisted. Both must agree.
	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, co := range companies {
		if co.RecordedBy != nil {
			t.Fatalf("stored company %s recorded_by = %q, want nil", co.CompanyID, *co.RecordedBy)
		}
	}
	wgs, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, wg := range wgs {
		if wg.RecordedBy != nil {
			t.Fatalf("stored workgroup %s recorded_by = %q, want nil", wg.TradeID, *wg.RecordedBy)
		}
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, per := range persons {
		if per.RecordedBy != nil {
			t.Fatalf("stored person %s recorded_by = %q, want nil", per.PersonID, *per.RecordedBy)
		}
	}
}

// --- the add-on-demand path: THE site that actually matters (a person can
// land on an evacuation muster with no record of who added them) ---

func TestGateCheckInAddOnDemandAttributesRecordedByOnCompanyTradeAndPerson(t *testing.T) {
	st := newDomainTestStore(t)
	post := postPtr("Ice Road")

	_, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver: PersonSlot{
			NewName: "Walk In Wanda", NewCompany: "Brand New Co", NewTrade: "Brand New Crew",
		},
		RecordedBy: post,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 1 || companies[0].Name != "Brand New Co" {
		t.Fatalf("companies = %+v, want exactly 1 named %q", companies, "Brand New Co")
	}
	if companies[0].RecordedBy == nil || *companies[0].RecordedBy != "Ice Road" {
		t.Fatalf("add-on-demand company recorded_by = %v, want \"Ice Road\"", companies[0].RecordedBy)
	}

	wgs, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(wgs) != 1 || wgs[0].Name != "Brand New Crew" {
		t.Fatalf("workgroups = %+v, want exactly 1 named %q", wgs, "Brand New Crew")
	}
	if wgs[0].RecordedBy == nil || *wgs[0].RecordedBy != "Ice Road" {
		t.Fatalf("add-on-demand workgroup recorded_by = %v, want \"Ice Road\"", wgs[0].RecordedBy)
	}

	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 || persons[0].Name != "Walk In Wanda" {
		t.Fatalf("persons = %+v, want exactly 1 named %q", persons, "Walk In Wanda")
	}
	if persons[0].RecordedBy == nil || *persons[0].RecordedBy != "Ice Road" {
		t.Fatalf("add-on-demand person recorded_by = %v, want \"Ice Road\" — this is exactly F2's scenario: a person minted mid-check-in with no record of who added them", persons[0].RecordedBy)
	}
}

func TestGateCheckInAddOnDemandRecordedByIsNilWhenNotAttributed(t *testing.T) {
	st := newDomainTestStore(t)

	_, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14","⟧
		VehicleID:  w7SeedVehicleID(t, st, "site-a", "TRK-14"),
		Driver:     PersonSlot{NewName: "Nameless Nolan", NewCompany: "No Officer Co"},
		RecordedBy: nil, // unattributed: the nullable contract, still expressible
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 1 || companies[0].RecordedBy != nil {
		t.Fatalf("add-on-demand company recorded_by = %+v, want nil (unattributed, never a fabricated placeholder)", companies)
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 || persons[0].RecordedBy != nil {
		t.Fatalf("add-on-demand person recorded_by = %+v, want nil (unattributed, never a fabricated placeholder)", persons)
	}
}

// --- ref_alias: "an alias merges two identities on a roster, so who did
// that is at least as interesting" (fix brief) ---

// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A (sites 1–2) — DISPOSITION (1), RETIRE.
// Every superseded line is preserved verbatim below; nothing was deleted.
//
// WHAT IT PROVED: that an ATTRIBUTED merge landed `recorded_by = "Ice Road"` on the
// `ref_alias` TABLE — i.e. it asserted the deleted mechanism WORKS.
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 deletes the ref_alias mechanism in full
// (event type, projection, table, fold, CreateAlias/CreateAliasRecordedBy); [[D-47]]
// bars the event type from v10's frozen vocabulary. Both the author it called and the
// table it read are gone, so there is nothing left to substitute.
//
// ⭐ COVERAGE COST, STATED HONESTLY AND CHECKED — this is a REDUCTION, not a tidy-up:
// the intent "a merge records the post that made it" has NO successor to re-express
// against. `AmendCompany` takes no recordedBy and has no `…RecordedBy` variant, and
// `companyAmendmentRow` carries no such field — deliberately: company_amend.go:19-21
// says "CompanyCreated DOES carry RecordedBy (refdata.go:26); the amendment carrier
// deliberately does NOT (W5d-2b) — attribution on an amendment is STRUCTURAL,
// env.SiteID, never a payload field (D-5, S7)", and :69-70 records `recorded_by` was
// "STRUCK from that freeze list at rev 4". So the attribution did not move to a new
// payload field this test could follow; it left the payload by ruling. Nothing to keep.

// func ⟦was: "TestCreateAliasRecordedByAttributesThePost"⟧(t *testing.T) {
// 	st := newDomainTestStore(t)
// 	c1, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	c2, err := CreateCompany(st, "site-a", 1, "Kakavik", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if _, err := CreateAliasRecordedBy(st, "site-a", 1, c2.CompanyID, c1.CompanyID, postPtr("Ice Road"), 0, "default"); err != nil {
// 		t.Fatal(err)
// 	}
// 	aliases, err := dumpSection[RefAlias](st, "ref_alias")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if len(aliases) != 1 || aliases[0].RecordedBy == nil || *aliases[0].RecordedBy != "Ice Road" {
// 		t.Fatalf("ref_alias recorded_by = %+v, want \"Ice Road\"", aliases)
// 	}
// }

// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A (sites 3–4) — DISPOSITION (1), RETIRE.
// Superseded lines preserved verbatim below.
//
// WHAT IT PROVED: the NEGATIVE half of the pair above — that the UNATTRIBUTED author
// left `ref_alias.recorded_by` a genuine SQL NULL rather than "" or a placeholder.
// A distinct assertion from its sibling, retired for its own reason rather than as
// part of a sweep: its subject is the same deleted TABLE, read through the same
// deleted dump section, and its author (`CreateAlias`) is deleted too.
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 / [[D-47]], as above.
//
// ⭐ COVERAGE COST: the nil-vs-empty-string discipline it pinned is NOT re-expressible
// here for the same measured reason as its sibling — the amendment carrier has no
// `recorded_by` field at all (W5d-2b), so there is no column on which the
// NULL-vs-placeholder distinction could still be posed for a merge.

// func ⟦was: "TestCreateAliasRecordedByIsNilWhenNotAttributed"⟧(t *testing.T) {
// 	st := newDomainTestStore(t)
// 	c1, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	c2, err := CreateCompany(st, "site-a", 1, "Kakavik", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	// Old unattributed CreateAlias: unchanged behaviour, still NULL.
// 	if _, err := CreateAlias(st, "site-a", 1, c2.CompanyID, c1.CompanyID, 0, "default"); err != nil {
// 		t.Fatal(err)
// 	}
// 	aliases, err := dumpSection[RefAlias](st, "ref_alias")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if len(aliases) != 1 || aliases[0].RecordedBy != nil {
// 		t.Fatalf("ref_alias recorded_by = %+v, want nil", aliases)
// 	}
// }

// ⟦was: "TestRefAliasRecordedByTiebreakIsOrderIndependent"⟧ is F2's own analogue of
// alias.go's existing sharedConflictDuplicateID case (determinism_test.go):
// two sites independently author conflicting aliases for the SAME
// duplicate_id before either sees the other's decision. applyRefAliasCreated
// resolves canonical_id via min() — order-independent by construction — and
// F2's recorded_by must track WHICHEVER row's canonical_id wins that
// tiebreak, not "whichever event applied last" (that would be a NEW
// order-dependence bug of exactly the shape this fold already exists to
// refuse). Applies the same two synthetic foreign events in BOTH orders
// against two fresh stores and asserts both land on the identical
// (canonical_id, recorded_by) pair.
// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND A (sites 5–6) — DISPOSITION (1), RETIRE.
// Superseded lines preserved verbatim below.
//
// WHAT IT PROVED: TWO things, and they have DIFFERENT fates — which is why this site
// is retired on its own reasoning and not with its two siblings above.
//   (i) that `applyRefAliasCreated` resolved a cross-site canonical_id conflict by
//       min(), order-independently; and
//  (ii) that `recorded_by` tracked the canonical_id WINNER rather than the last
//       delivered event.
// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 deletes the FOLD itself. Unlike its
// siblings this asserted about the fold, not the table.
//
// ⭐ (i) IS NOT LOST — it survives elsewhere, and that is measured rather than assumed:
// the composed merge is order-independent by a DIFFERENT mechanism (ascending record
// order — company_amend.go:398-415, "last assertion in record order wins; rows arrive
// already in ascending key order"), and that property has its own live test:
// TestW5dReversalOrderIndependentAcrossStreams (w5d_reversal_test.go:90), which permutes
// cross-stream delivery of one event set and requires byte-identical state and identical
// composed reads. So retiring this costs NOTHING on limb (i).
//
// ⭐ COVERAGE COST is limb (ii) ONLY, and it is unrecoverable for the same measured
// reason as the two siblings: the amendment carrier has no `recorded_by` (W5d-2b), so
// "attribution tracks the tiebreak winner" has no surviving surface to be posed on.

// func ⟦was: "TestRefAliasRecordedByTiebreakIsOrderIndependent"⟧(t *testing.T) {
// 	dup := "shared-dup-company"
// 	lo := "site-a-company-1111111111111111" // lexicographically smaller: wins min()
// 	hi := "site-b-company-2222222222222222"
//
// 	mkEnv := func(t *testing.T, siteID, canonical, recordedBy string) store.Envelope {
// 		t.Helper()
// 		b, err := json.Marshal(RefAliasCreated{
// 			Type: "ref_alias_created", DuplicateID: dup, CanonicalID: canonical, RecordedBy: &recordedBy,
// 		})
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		return store.Envelope{
// 			SiteID: siteID, NodeEpoch: 1, Seq: 1,
// 			SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
// 			Payload: b,
// 		}
// 	}
//
// 	envLo := func(t *testing.T) store.Envelope { return mkEnv(t, "site-a", lo, "Ice Road") }
// 	envHi := func(t *testing.T) store.Envelope { return mkEnv(t, "site-b", hi, "Pad Site Control") }
//
// 	assertWinner := func(t *testing.T, st *store.Store) {
// 		t.Helper()
// 		aliases, err := dumpSection[RefAlias](st, "ref_alias")
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		if len(aliases) != 1 {
// 			t.Fatalf("ref_alias rows = %d, want 1", len(aliases))
// 		}
// 		if aliases[0].CanonicalID != lo {
// 			t.Fatalf("canonical_id = %s, want %s (min() tiebreak)", aliases[0].CanonicalID, lo)
// 		}
// 		if aliases[0].RecordedBy == nil || *aliases[0].RecordedBy != "Ice Road" {
// 			t.Fatalf("recorded_by = %v, want \"Ice Road\" (must track the canonical_id winner, not delivery order)", aliases[0].RecordedBy)
// 		}
// 	}
//
// 	t.Run("lo-then-hi", func(t *testing.T) {
// 		st := newDomainTestStore(t)
// 		if applied, err := st.Apply(envLo(t)); err != nil || !applied {
// 			t.Fatalf("apply lo: applied=%v err=%v", applied, err)
// 		}
// 		if applied, err := st.Apply(envHi(t)); err != nil || !applied {
// 			t.Fatalf("apply hi: applied=%v err=%v", applied, err)
// 		}
// 		assertWinner(t, st)
// 	})
//
// 	t.Run("hi-then-lo", func(t *testing.T) {
// 		st := newDomainTestStore(t)
// 		if applied, err := st.Apply(envHi(t)); err != nil || !applied {
// 			t.Fatalf("apply hi: applied=%v err=%v", applied, err)
// 		}
// 		if applied, err := st.Apply(envLo(t)); err != nil || !applied {
// 			t.Fatalf("apply lo: applied=%v err=%v", applied, err)
// 		}
// 		assertWinner(t, st)
// 	})
// }
