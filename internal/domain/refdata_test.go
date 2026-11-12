// refdata_test.go — company/workgroup/person done-when tests (chunk-plan C5):
// open-set duplicate coexistence with no arbitration (design §8.7 case 2),
// the workgroup-uniqueness residue (chunk-carry-forward row 11: an explicit
// executable assert was required, not left bare a second time), and
// site-prefixed identity.
package domain

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

func newDomainTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := Register(st); err != nil {
		t.Fatalf("register: %v", err)
	}
	return st
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// --- 8.7 case 2: duplicate reference-data inserts, both rows survive ---

func TestCompanyOpenSetDuplicatesCoexistNoArbitration(t *testing.T) {
	st := newDomainTestStore(t)

	local, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatalf("create local company: %v", err)
	}

	// A second post minting the SAME name independently, delivered as if
	// replicated (store.Apply is exactly what a peer's pull would call —
	// the injected-foreign-event mechanism C2 built so C5 is buildable
	// before replication exists, chunk-plan C2 done-when (d)).
	foreignID := newID("site-b", "company")
	env := store.Envelope{
		SiteID: "site-b", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-b",
		Payload: mustJSON(t, CompanyCreated{Type: "company_created", CompanyID: foreignID, Name: "Kakivik"}),
	}
	applied, err := st.Apply(env)
	if err != nil || !applied {
		t.Fatalf("apply foreign company: applied=%v err=%v", applied, err)
	}

	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(companies) != 2 {
		t.Fatalf("companies = %d, want 2 (both Kakiviks coexisting, no arbitration)", len(companies))
	}
	if local.CompanyID == foreignID {
		t.Fatalf("local and foreign company IDs collided: %s", local.CompanyID)
	}
	seenNames := map[string]int{}
	for _, c := range companies {
		seenNames[c.Name]++
	}
	if seenNames["Kakivik"] != 2 {
		t.Fatalf("expected 2 rows named Kakivik (no merge, no arbitration), got %d", seenNames["Kakivik"])
	}
}

// --- chunk-carry-forward row 11: workgroup unique on company+name, the
// bare-spec-sentence residue that needed an explicit executable assert ---

// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ THE ARITY FOSSIL — KIND A, DISPOSITION (1).
// Superseded lines preserved verbatim below. ⚠ THE NAME IS DELIBERATELY NOT CHANGED:
// §60 rules it is EVIDENCE OF WHAT WAS REMOVED and must survive in this note.
//
// WHAT IT PROVED — and its NAME is the assertion: that a trade was unique on
// COMPANY + NAME. Its body states both halves: "Duplicate under the SAME company:
// refused" and "The SAME name under a DIFFERENT company: accepted (company-scoped,
// not global)", closing with a count of exactly 2 trades.
// WHICH RULING REMOVED ITS SUBJECT: ⟦DR-W8-2⟧ — refdata.go:44 — "a trade is a site-wide
// open set and is not scoped to a company. The predecessor's company predicate is
// DELETED, not carried, and its absence is asserted (W8-2(a))". `Trade` has no
// CompanyID field at all (refdata.go:202-214).
//
// ⚠ THIS IS WHY IT IS RETIRED RATHER THAN ARITY-REPAIRED, and it is the one site where
// the mechanical repair applied at 23 others is WRONG: dropping the company argument
// makes the third call byte-identical to the first, so it is REFUSED, the "different
// company should be accepted" arm FAILS, and the count is 1 rather than 2. The
// assertion did not become uncompilable — it became FALSE, and W8 asserts its negation.
//
// ⭐ COVERAGE COST: uniqueness is not unguarded — the SAME-company duplicate refusal it
// also proved survives, unchanged and still running, in TestTradeRefusalDoesNotAuthor
// immediately below, which now reads as plain name-uniqueness. What is lost is only the
// company-scoped half, which is the half W8-2(a) asserts the ABSENCE of.

// func ⟦was: "TestTradeUniqueOnCompanyPlusName"⟧(t *testing.T) {
// 	st := newDomainTestStore(t)
// 	c1, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	c2, err := CreateCompany(st, "site-a", 1, "Other Co", 0, "default")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
//
// 	if _, err := CreateTrade(st, "site-a", 1, c1.CompanyID, "Roughnecks", 0, "default"); err != nil {
// 		t.Fatalf("first workgroup creation: %v", err)
// 	}
// 	// Duplicate under the SAME company: refused.
// 	if _, err := CreateTrade(st, "site-a", 1, c1.CompanyID, "Roughnecks", 0, "default"); err == nil {
// 		t.Fatal("expected refusal creating a duplicate workgroup under the same company")
// 	}
// 	// The SAME name under a DIFFERENT company: accepted (company-scoped, not global).
// 	if _, err := CreateTrade(st, "site-a", 1, c2.CompanyID, "Roughnecks", 0, "default"); err != nil {
// 		t.Fatalf("same name under a different company should be accepted: %v", err)
// 	}
//
// 	wgs, err := Trades(st)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if len(wgs) != 2 {
// 		t.Fatalf("workgroups = %d, want 2 (the refused duplicate must not have been written)", len(wgs))
// 	}
// }

func TestTradeRefusalDoesNotAuthor(t *testing.T) {
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
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "if _, err := CreateTrade(st, "site-a", 1, c.CompanyID, "Roughnecks", 0, "default"); err != nil {"⟧
	if _, err := CreateTrade(st, "site-a", 1, "Roughnecks", 0, "default"); err != nil {
		t.Fatal(err)
	}
	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "if _, err := CreateTrade(st, "site-a", 1, c.CompanyID, "Roughnecks", 0, "default"); err == nil {"⟧
	if _, err := CreateTrade(st, "site-a", 1, "Roughnecks", 0, "default"); err == nil {
		t.Fatal("expected refusal")
	}
	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Fatalf("watermark advanced on a refused creation: before=%d after=%d — refusal must never call Author", wmBefore, wmAfter)
	}
}

// --- person: open set, no dedup, roster default is genuinely optional ---

func TestCreatePersonWithDefaults(t *testing.T) {
	st := newDomainTestStore(t)
	c, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "w, err := CreateTrade(st, "site-a", 1, c.CompanyID, "Roughnecks", 0, "default")"⟧
	w, err := CreateTrade(st, "site-a", 1, "Roughnecks", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := CreatePerson(st, "site-a", 1, "Alice Anderson", &c.CompanyID, &w.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 {
		t.Fatalf("persons = %d, want 1", len(persons))
	}
	if persons[0].PersonID != p.PersonID {
		t.Fatalf("person id = %s, want %s", persons[0].PersonID, p.PersonID)
	}
	if persons[0].CompanyID == nil || *persons[0].CompanyID != c.CompanyID {
		t.Fatalf("person company default = %v, want %s", persons[0].CompanyID, c.CompanyID)
	}
	if persons[0].TradeID == nil || *persons[0].TradeID != w.TradeID {
		t.Fatalf("person workgroup default = %v, want %s", persons[0].TradeID, w.TradeID)
	}
}

func TestCreatePersonWithoutCompanyIsAllowed(t *testing.T) {
	st := newDomainTestStore(t)
	if _, err := CreatePerson(st, "site-a", 1, "No Employer Yet", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 1 {
		t.Fatalf("persons = %d, want 1", len(persons))
	}
	if persons[0].CompanyID != nil {
		t.Fatalf("expected nil company default, got %v", persons[0].CompanyID)
	}
}

// --- identity: site-prefixed IDs (design §7) ---

func TestGeneratedIDsAreSitePrefixed(t *testing.T) {
	st := newDomainTestStore(t)
	c, err := CreateCompany(st, "site-a", 1, "Acme", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.CompanyID, "site-a-company-") {
		t.Fatalf("company id %q not site-prefixed", c.CompanyID)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "w, err := CreateTrade(st, "site-a", 1, c.CompanyID, "Crew", 0, "default")"⟧
	w, err := CreateTrade(st, "site-a", 1, "Crew", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §85, 2026-09-03⟧ LOCK-TIME expectation re-pointed; THE PROPERTY IS KEPT.
	// ⚠ THE FAILURE MESSAGE WAS MISLEADING AND THAT IS THE FINDING: it read
	// `workgroup id "site-a-trade-…" not site-prefixed` — but that id IS site-prefixed.
	// The standing property this test is NAMED for (design §7: generated ids carry their
	// authoring site) HOLDS, unbroken by W8. What encoded the old vocabulary was the
	// EXPECTATION: it looked for a `workgroup`-shaped id and met a `trade`-shaped one.
	// ⭐ Its sibling arms (company, person) never failed and are untouched — which is the
	// evidence that the site-prefix property itself was never at issue.
	// ⟦was: "if !strings.HasPrefix(w.TradeID, \"site-a-workgroup-\") {"⟧
	// ⟦was: "\tt.Fatalf(\"workgroup id %q not site-prefixed\", w.TradeID)"⟧
	if !strings.HasPrefix(w.TradeID, "site-a-trade-") {
		t.Fatalf("trade id %q not site-prefixed", w.TradeID)
	}
	p, err := CreatePerson(st, "site-a", 1, "Bob", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.PersonID, "site-a-person-") {
		t.Fatalf("person id %q not site-prefixed", p.PersonID)
	}
}
