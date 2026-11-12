// witness_d82_rename_guard_test.go — [[D-82]] (2026-09-05): renaming a trade
// onto a name already on the list is REFUSED. Same rule as creating.
//
// [[D-76]] closed the CREATE door with sameTradeName (case + leading/trailing
// space fold) and a typed refusal. The RENAME door — AmendTrade's `name`
// fact — had no duplicate check of any kind: with Pipefitter listed,
// AmendTrade(Catering → "Pipefitter") returned err = nil and Trades() then
// held two rows named exactly Pipefitter — two identical headings on the
// evacuation muster sheet, through the amendment door. Measured by the
// d76-trade-fold pass, reported not fixed; produced again on the clean tree
// of this branch before this file was written (see
// reports/witness/TESTER-d82.md §6, M1).
//
// The guard compares the corrected name against the COMPOSED list (Trades())
// — the names the muster actually prints — never against base rows, and
// never against the trade being renamed itself. The three positive controls
// below are as load-bearing as the refusals: a genuinely new name still
// records, a trade's own name (or a case correction of it) is not a duplicate
// of itself, and a name FREED by an earlier rename is takeable. And the
// operation D-82 forbids folding into a rename — the merge — is proven still
// available as its own `same_as` amendment.
//
// ⭐ [[D-66]] direction: THE GATE NEVER REFUSES. A rename guard can only break
// D-66 if a gate path reaches a rename. TestD82NoGateOrCheckInPathReachesARename
// is the standing census that none does, and
// TestD82TheGateStillResolvesBothNamesAfterARefusedRename drives the real
// GateCheckIn through both names afterwards.
package domain

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// d82Rename asserts trade `id`'s name to `name` through the real authoring
// edge and returns the error, unexamined.
func d82Rename(st *store.Store, id, name string) error {
	_, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: id, Name: &name}, 0, "default")
	return err
}

// d82Named returns every COMPOSED trade whose name is exactly `name` — the
// muster-heading view. Callers assert on identity (TradeID), not on count.
func d82Named(t *testing.T, st *store.Store, name string) []Trade {
	t.Helper()
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var hits []Trade
	for _, tr := range trades {
		if tr.Name == name {
			hits = append(hits, tr)
		}
	}
	return hits
}

// d82Watermark is the site-a/epoch-1 authoring watermark — a refusal never
// calls Author, so it must not move across a refused rename.
func d82Watermark(t *testing.T, st *store.Store) int64 {
	t.Helper()
	wm, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// d82TwoTrades seeds Pipefitter and Catering and returns them.
func d82TwoTrades(t *testing.T, st *store.Store) (pipefitter, catering Trade) {
	t.Helper()
	var err error
	if pipefitter, err = CreateTrade(st, "site-a", 1, "Pipefitter", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if catering, err = CreateTrade(st, "site-a", 1, "Catering", 0, "default"); err != nil {
		t.Fatal(err)
	}
	return pipefitter, catering
}

// --- the refusal ------------------------------------------------------------

// TestD82RenameOntoAListedNameIsRefusedByTypeNamingTheHolderAndAuthorsNothing
// is the ruling's centre: with Pipefitter listed, renaming Catering to
// "Pipefitter" is refused with a typed *TradeRenameRefusal whose Existing IS
// the Pipefitter row (identity, not name), the refusal is ALSO a
// *TradeAmendRefusal by errors.As (the fact-domain class web/trade.go's
// partition matches), its officer sentence names the holder's id and on-list
// spelling, the watermark does not move, and Catering is still named
// Catering — refused, not merged.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): the wording below was
// never literally producible — this file names *TradeRenameRefusal, which
// does not exist on any pre-D-82 tree, so it cannot compile there, let alone
// run and "produce" a runtime result. The underlying claim is nonetheless
// TRUE: MEASURED via a minimal probe (no reference to *TradeRenameRefusal)
// run on pre-fix ancestor `087711c`: `AmendTrade(Catering → "Pipefitter")
// with Pipefitter listed = <nil>`, and two rows named exactly Pipefitter.
// See reports/witness/CITATION-AUDIT.md row 1.
func TestD82RenameOntoAListedNameIsRefusedByTypeNamingTheHolderAndAuthorsNothing(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter, catering := d82TwoTrades(t, st)
	wmBefore := d82Watermark(t, st)

	err := d82Rename(st, catering.TradeID, "Pipefitter")

	var refusal *TradeRenameRefusal
	if !errors.As(err, &refusal) {
		t.Errorf("AmendTrade(Catering → \"Pipefitter\") with Pipefitter listed = %v, want a *TradeRenameRefusal (D-82: refused, same rule as creating)", err)
	} else {
		if refusal.Existing.TradeID != pipefitter.TradeID {
			t.Errorf("refusal.Existing.TradeID = %q, want the Pipefitter row %q — the trade already holding the name, so the officer can pick it", refusal.Existing.TradeID, pipefitter.TradeID)
		}
		if refusal.Existing.Name != "Pipefitter" {
			t.Errorf("refusal.Existing.Name = %q, want the on-list spelling \"Pipefitter\"", refusal.Existing.Name)
		}
		if refusal.Renamed.TradeID != catering.TradeID || refusal.Typed != "Pipefitter" {
			t.Errorf("refusal.Renamed.TradeID, Typed = %q, %q, want %q, \"Pipefitter\"", refusal.Renamed.TradeID, refusal.Typed, catering.TradeID)
		}
		if !strings.Contains(refusal.Reason(), pipefitter.TradeID) || !strings.Contains(refusal.Reason(), `"Pipefitter"`) {
			t.Errorf("the officer sentence does not name the holder's id and on-list spelling: %q", refusal.Reason())
		}
	}
	var amend *TradeAmendRefusal
	if !errors.As(err, &amend) {
		t.Errorf("errors.As(err, **TradeAmendRefusal) = false — the rename refusal must be in the FACT-DOMAIN class by type, or web/trade.go's partition answers a 500: %v", err)
	} else if refusal != nil && amend.Reason != refusal.Reason() {
		t.Errorf("the unwrapped *TradeAmendRefusal.Reason = %q, want the rename refusal's own sentence %q", amend.Reason, refusal.Reason())
	}
	var input *TradeAmendInputError
	if errors.As(err, &input) {
		t.Errorf("errors.As(err, **TradeAmendInputError) = true — a name collision is not a mistyped id: %v", err)
	}

	if wmAfter := d82Watermark(t, st); wmAfter != wmBefore {
		t.Errorf("watermark advanced on a refused rename: %d -> %d — a refusal never calls Author", wmBefore, wmAfter)
	}
	if hits := d82Named(t, st, "Pipefitter"); len(hits) != 1 || hits[0].TradeID != pipefitter.TradeID {
		t.Errorf("rows named exactly Pipefitter after the rename = %+v, want exactly the original row %q — two would be two identical muster headings", hits, pipefitter.TradeID)
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != catering.TradeID {
		t.Errorf("Catering after the refused rename = %+v, want still one row %q named Catering — refused, not merged", hits, catering.TradeID)
	}
	if got, err := TradeByID(st, catering.TradeID); err != nil || got == nil || got.SameAs != nil {
		t.Errorf("Catering.SameAs after the refused rename = %v (err %v), want nil — a rename onto a listed name must never become an implicit merge (D-30(a) is its own operation)", got, err)
	}
}

// TestD82RenameOntoACaseOrPaddingVariantOfAListedNameIsRefused is D-76's
// fold applied at this door: every spelling the create door refuses, the
// rename door refuses, and each refusal names the same holder row.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): same compile-impossible
// shape as line 100's citation above — this file cannot run on any pre-D-82
// tree. MEASURED via a minimal probe (no reference to *TradeRenameRefusal)
// run on pre-fix ancestor `087711c`: all four retypes returned <nil>; the
// composed rows afterward were Pipefitter (original) and "pIpEfItTeR\t"
// (Catering's last retype, stored verbatim) — two headings that fold to
// Pipefitter under case/whitespace normalization, exactly as claimed. See
// reports/witness/CITATION-AUDIT.md row 2 (there marked INHERITED,
// re-measured directly by me this pass).
func TestD82RenameOntoACaseOrPaddingVariantOfAListedNameIsRefused(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter, catering := d82TwoTrades(t, st)
	for _, retype := range []string{"pipefitter", "  Pipefitter  ", "PIPEFITTER", "pIpEfItTeR\t"} {
		wmBefore := d82Watermark(t, st)
		err := d82Rename(st, catering.TradeID, retype)
		var refusal *TradeRenameRefusal
		if !errors.As(err, &refusal) {
			t.Errorf("AmendTrade(Catering → %q) with Pipefitter listed = %v, want a *TradeRenameRefusal", retype, err)
			continue
		}
		if refusal.Existing.TradeID != pipefitter.TradeID || refusal.Existing.Name != "Pipefitter" {
			t.Errorf("AmendTrade(Catering → %q): refusal.Existing = %q %q, want the Pipefitter row %q", retype, refusal.Existing.TradeID, refusal.Existing.Name, pipefitter.TradeID)
		}
		if wmAfter := d82Watermark(t, st); wmAfter != wmBefore {
			t.Errorf("AmendTrade(Catering → %q): watermark advanced %d -> %d on a refusal", retype, wmBefore, wmAfter)
		}
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != catering.TradeID {
		t.Errorf("Catering after four refused retypes = %+v, want still the one row %q named Catering", hits, catering.TradeID)
	}
}

// TestD82RenameOntoANameThatItselfArrivedByRenameIsRefused is why the guard
// reads the COMPOSED list and not base rows: rename Welder → Pipefitter, then
// try Catering → Pipefitter. No base row is named Pipefitter — only the
// composed list, which is what the muster prints, knows Welder now holds
// it. A guard over base rows (createTrade's read, D-76 §6 🔴1) would pass
// this and print two Pipefitter headings.
//
// CORRECTED CITATION (2026-09-06, evidence-auditor): same compile-impossible
// shape as lines 100/156 above. MEASURED via a minimal probe (no reference
// to *TradeRenameRefusal) run on pre-fix ancestor `087711c`:
// AmendTrade(Catering → "Pipefitter") after Welder was renamed to
// "Pipefitter" = <nil>, and two composed rows named exactly Pipefitter (the
// renamed Welder row and Catering). See reports/witness/CITATION-AUDIT.md
// row 3 (there marked INHERITED, re-measured directly by me this pass).
func TestD82RenameOntoANameThatItselfArrivedByRenameIsRefused(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	catering, err := CreateTrade(st, "site-a", 1, "Catering", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := d82Rename(st, welder.TradeID, "Pipefitter"); err != nil {
		t.Fatalf("test setup: Welder → Pipefitter (a free name) = %v, want recorded", err)
	}

	err = d82Rename(st, catering.TradeID, "Pipefitter")
	var refusal *TradeRenameRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("AmendTrade(Catering → \"Pipefitter\") after Welder was RENAMED to Pipefitter = %v, want a *TradeRenameRefusal — the holder exists only on the composed list", err)
	}
	if refusal.Existing.TradeID != welder.TradeID || refusal.Existing.Name != "Pipefitter" {
		t.Errorf("refusal.Existing = %q %q, want the renamed Welder row %q under its composed name \"Pipefitter\"", refusal.Existing.TradeID, refusal.Existing.Name, welder.TradeID)
	}
	if hits := d82Named(t, st, "Pipefitter"); len(hits) != 1 || hits[0].TradeID != welder.TradeID {
		t.Errorf("rows named Pipefitter = %+v, want exactly the renamed Welder row %q", hits, welder.TradeID)
	}
}

// --- the positive controls: it must not over-refuse ---------------------------

// TestD82RenameToAGenuinelyNewNameStillRecords: a guard that refused every
// rename would pass the tests above. Green on the clean tree by design.
func TestD82RenameToAGenuinelyNewNameStillRecords(t *testing.T) {
	st := newDomainTestStore(t)
	_, catering := d82TwoTrades(t, st)
	wmBefore := d82Watermark(t, st)
	if err := d82Rename(st, catering.TradeID, "Scaffolding"); err != nil {
		t.Fatalf("AmendTrade(Catering → \"Scaffolding\") with only Pipefitter and Catering listed = %v, want recorded — a new name is not a duplicate of anything", err)
	}
	if wmAfter := d82Watermark(t, st); wmAfter != wmBefore+1 {
		t.Errorf("watermark %d -> %d, want exactly one authored event", wmBefore, wmAfter)
	}
	if hits := d82Named(t, st, "Scaffolding"); len(hits) != 1 || hits[0].TradeID != catering.TradeID {
		t.Errorf("rows named Scaffolding = %+v, want the Catering row %q under its new name", hits, catering.TradeID)
	}
}

// TestD82RenameToItsOwnCurrentNameOrACaseCorrectionOfItIsNotADuplicateOfItself:
// the trade being renamed holds its own name, and under the fold it also
// "holds" every case variant of it. Neither may be caught as a duplicate.
// The self-rename returned nil on the clean tree (measured) and must go on
// doing so; the case correction "welder" → "Welder" is the legitimate
// clean-up D-76 makes wanted, and it must land.
func TestD82RenameToItsOwnCurrentNameOrACaseCorrectionOfItIsNotADuplicateOfItself(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter, catering := d82TwoTrades(t, st)
	if err := d82Rename(st, catering.TradeID, "Catering"); err != nil {
		t.Errorf("AmendTrade(Catering → \"Catering\"), its own current name, = %v, want nil as on the clean tree — a trade is not a duplicate of itself", err)
	}
	if hits := d82Named(t, st, "Catering"); len(hits) != 1 || hits[0].TradeID != catering.TradeID {
		t.Errorf("rows named Catering = %+v, want the one row %q", hits, catering.TradeID)
	}

	lower, err := CreateTrade(st, "site-a", 1, "welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := d82Rename(st, lower.TradeID, "Welder"); err != nil {
		t.Fatalf("AmendTrade(welder → \"Welder\"), a case correction of its OWN name, = %v, want recorded", err)
	}
	if hits := d82Named(t, st, "Welder"); len(hits) != 1 || hits[0].TradeID != lower.TradeID {
		t.Errorf("rows named Welder = %+v, want the corrected row %q", hits, lower.TradeID)
	}
	// And Pipefitter, untouched throughout, is still exactly itself.
	if hits := d82Named(t, st, "Pipefitter"); len(hits) != 1 || hits[0].TradeID != pipefitter.TradeID {
		t.Errorf("rows named Pipefitter = %+v, want the untouched row %q", hits, pipefitter.TradeID)
	}
}

// TestD82RenameOntoANameFreedByAnEarlierRenameIsAllowed is the other half of
// reading the composed list: after Welder → Pipefitter, no heading says
// Welder, so Catering → Welder is a rename onto a free name. A guard over
// base rows would refuse it, naming a trade that no longer holds the name.
func TestD82RenameOntoANameFreedByAnEarlierRenameIsAllowed(t *testing.T) {
	st := newDomainTestStore(t)
	welder, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	catering, err := CreateTrade(st, "site-a", 1, "Catering", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := d82Rename(st, welder.TradeID, "Pipefitter"); err != nil {
		t.Fatalf("test setup: Welder → Pipefitter = %v", err)
	}
	if hits := d82Named(t, st, "Welder"); len(hits) != 0 {
		t.Fatalf("precondition: rows still named Welder = %+v, want none", hits)
	}
	if err := d82Rename(st, catering.TradeID, "Welder"); err != nil {
		t.Fatalf("AmendTrade(Catering → \"Welder\") after Welder was renamed away = %v, want recorded — the name is free on the list the muster prints", err)
	}
	if hits := d82Named(t, st, "Welder"); len(hits) != 1 || hits[0].TradeID != catering.TradeID {
		t.Errorf("rows named Welder = %+v, want the Catering row %q", hits, catering.TradeID)
	}
}

// TestD82TheSeparateSameAsMergeIsStillAvailableAndARenameNeverPerformsIt:
// D-82 says refuse, do NOT merge — because merging is its own operation
// ([[D-30]](a)). Both halves are proven: the refused rename left no merge
// pointer (asserted in the first test), and the explicit `same_as` merge of
// the same two trades still records.
func TestD82TheSeparateSameAsMergeIsStillAvailableAndARenameNeverPerformsIt(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter, catering := d82TwoTrades(t, st)
	if err := d82Rename(st, catering.TradeID, "Pipefitter"); err == nil {
		t.Fatal("precondition: the rename onto Pipefitter was not refused")
	}
	target := pipefitter.TradeID
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: catering.TradeID, SameAs: &target}, 0, "default"); err != nil {
		t.Fatalf("AmendTrade(Catering same_as Pipefitter) = %v, want recorded — the merge is the sanctioned way to make two crews one, and the rename guard must not touch it", err)
	}
	got, err := TradeByID(st, catering.TradeID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SameAs == nil || *got.SameAs != pipefitter.TradeID {
		t.Errorf("Catering.SameAs after the explicit merge = %v, want %q", got.SameAs, pipefitter.TradeID)
	}
	if got.Name != "Catering" {
		t.Errorf("Catering's composed name after the refused rename and the merge = %q, want \"Catering\" — the refused rename recorded nothing", got.Name)
	}
}

// TestD82AmendingOnlyANonNameFactIsNotCheckedForACollision: an amendment
// that does not assert `name` (a handset number here) cannot collide with
// anything and must not read the list at all — it records as before.
func TestD82AmendingOnlyANonNameFactIsNotCheckedForACollision(t *testing.T) {
	st := newDomainTestStore(t)
	_, catering := d82TwoTrades(t, st)
	day := "555-0101"
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: catering.TradeID, PhoneDay: &day}, 0, "default"); err != nil {
		t.Fatalf("AmendTrade(Catering phone_day) with Pipefitter listed = %v, want recorded", err)
	}
	got, err := TradeByID(st, catering.TradeID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.PhoneDay == nil || *got.PhoneDay != day {
		t.Errorf("Catering.PhoneDay = %v, want %q", got.PhoneDay, day)
	}
}

// --- [[D-66]]: the gate never refuses -----------------------------------------

// TestD82TheGateStillResolvesBothNamesAfterARefusedRename drives the real
// GateCheckIn add-on-demand escape through BOTH names after Catering →
// Pipefitter was refused: "Pipefitter" resolves to the Pipefitter row and
// "Catering" to the Catering row, neither refused, nothing minted. The guard
// lives on the amendment edge and changes nothing the gate reads.
func TestD82TheGateStillResolvesBothNamesAfterARefusedRename(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter, catering := d82TwoTrades(t, st)
	if err := d82Rename(st, catering.TradeID, "Pipefitter"); err == nil {
		t.Fatal("precondition: the rename onto Pipefitter was not refused")
	}
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	before, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-82"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "Pipefitter Pat", NewTrade: "Pipefitter"},
			{NewName: "Catering Cal", NewTrade: "catering"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("gate check-in typing both names after the refused rename was REFUSED — D-66: the gate never refuses: %v", err)
	}
	after, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("trades after the check-in = %d, want %d — both names must resolve, minting nothing", len(after), len(before))
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Pipefitter Pat": pipefitter.TradeID, "Catering Cal": catering.TradeID}
	for _, p := range persons {
		id, ok := want[p.Name]
		if !ok {
			continue
		}
		delete(want, p.Name)
		if p.TradeID == nil || *p.TradeID != id {
			t.Errorf("%s's trade id = %s, want the existing row %q", p.Name, d76Deref(p.TradeID), id)
		}
	}
	for name := range want {
		t.Errorf("%s was not created by the check-in", name)
	}
}

// TestD82NoGateOrCheckInPathReachesARename is the rename-path CENSUS made
// standing. D-82's guard can break [[D-66]] only if a gate path can reach
// AmendTrade; today the ONLY production caller is web/trade.go's
// handleTradeAmend (the /trade admin screen), and the only builder of the
// trade_amended payload is AmendTrade itself. This walks every non-test Go
// file under app/ and fails, naming file:line, if any other caller appears —
// a rename wired into checkin.go or gate.go would surface here before it
// surfaced as a refusal at 04:10.
func TestD82NoGateOrCheckInPathReachesARename(t *testing.T) {
	root := w8AppRoot(t)
	allowed := map[string]bool{
		filepath.Join("internal", "domain", "trade_amend.go"): true, // the definition
		filepath.Join("internal", "web", "trade.go"):          true, // the one door
	}
	var offenders []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "AmendTrade(") || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "func AmendTrade(") {
				continue
			}
			seen[rel] = true
			if !allowed[rel] {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !seen[filepath.Join("internal", "web", "trade.go")] {
		t.Fatal("census surface broken: web/trade.go no longer calls AmendTrade( — the one known door has moved; re-census before trusting this test")
	}
	if len(offenders) != 0 {
		t.Errorf("AmendTrade is called outside the /trade admin screen — a rename door the D-82 guard was not proven against, and if it is a gate path, a D-66 refusal waiting to happen:\n  %s", strings.Join(offenders, "\n  "))
	}
}
