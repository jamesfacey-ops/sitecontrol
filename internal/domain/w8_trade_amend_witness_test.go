// w8_trade_amend_witness_test.go — witness/y7-trade: renders the officer-facing
// refusal sentence for three of AmendTrade's refusal arms (Q9, Q9b, Q9c), the
// census's "least-witnessed authoring edge in the commit" (22 of 40 blocks
// unreached — every refusal arm).
//
// Each test asserts what the officer actually reads on a refusal (the
// TradeAmendRefusal.Error() sentence) AND that nothing was appended to the
// log (W3-2: every refusal here is pre-append). Proven both ways per the
// witness brief: FAILS with its plant applied, PASSES without it — see
// reports/witness/y7-trade.md for the run transcript of both directions.
//
// Beyond the three census-assigned plants (Q9/Q9b/Q9c), the seven tests
// below work outward through the file's remaining unreached blocks,
// targeting AmendTrade's other refusal arms — none of which the census
// planted, so their own line-anchored mutants are authored here (same
// shape as plants.py) and reported in reports/witness/y7-trade.md.
package domain

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"errors"
	msqlite "modernc.org/sqlite"
	"sitecontrol/internal/store"
)

// TestWitnessY7Q9TradeAmendNoOpIsRefused witnesses trade_amend.go:598 (Q9):
// `if assertedCount == 0 && len(in.Clear) == 0 {`. An amendment that sets
// nothing and clears nothing must be refused, not minted — recording nothing
// is not a legitimate event. Plant Q9 (`if false && …`) disables the guard:
// the no-op amendment falls through every other arm untouched and is
// authored as a real, empty trade_amended event.
func TestWitnessY7Q9TradeAmendNoOpIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}

	before := len(w3Events(t, st, "site-a", 1))

	// Sets nothing, clears nothing.
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("Q9: a no-op trade amendment (nothing set, nothing cleared) was ACCEPTED — "+
			"trade_amend.go:598's guard did not fire. An officer must never be able to record a "+
			"no-op amendment against %q; recording nothing must be refused, not minted", tr.TradeID)
	}
	const want = "the amendment changes nothing (no field set, nothing cleared)"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Q9: refusal text = %q, want it to contain %q — this is the sentence the officer reads", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("Q9: the refused no-op amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7Q9bTradeAmendSelfMergeIsRefused witnesses trade_amend.go:627
// (Q9b): `if target == in.TradeID {`. A trade cannot be made an alias of
// itself. Plant Q9b (`if false && …`) disables the guard: the self-loop
// clears the kind check (a trade always resolves against itself), finds no
// prior merge (SameAs is nil), walks no cycle (there is nothing yet to
// cycle through), and is authored — a trade record that points at itself.
func TestWitnessY7Q9bTradeAmendSelfMergeIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}

	before := len(w3Events(t, st, "site-a", 1))

	self := tr.TradeID
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, SameAs: &self}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("Q9b: a same_as amendment naming %q AS ITS OWN TARGET was ACCEPTED — "+
			"trade_amend.go:627's guard did not fire. A trade heading on the muster must never be able "+
			"to alias itself", tr.TradeID)
	}
	const want = "a record cannot be merged with itself"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Q9b: refusal text = %q, want it to contain %q — this is the sentence the officer reads", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("Q9b: the refused self-merge appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7Q9cTradeAmendSecondMergeIsRefused witnesses trade_amend.go:644
// (Q9c): `if tr.SameAs != nil {`. A trade already merged into another cannot
// be merged again without first clearing same_as (the un-merge) — D-28/D-50's
// area: the muster must never fold merged people, and a trade heading that
// silently re-targets is the same shape of corruption. Plant Q9c (`if false
// && …`) disables the guard: a SECOND, contradictory same_as assertion is
// authored straight over the first with no refusal at all.
func TestWitnessY7Q9cTradeAmendSecondMergeIsRefused(t *testing.T) {
	st := w3NewStore(t)
	a, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateTrade(st, "site-a", 1, "Crane", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateTrade(st, "site-a", 1, "Rigging", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}

	// First merge: A -> B. This one is legitimate and must succeed (tr.SameAs
	// is nil at this point) — it is the precondition for the guard under test,
	// not part of what Q9c witnesses.
	bID := b.TradeID
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: a.TradeID, SameAs: &bID}, w3Base+w3Hour, "default"); err != nil {
		t.Fatalf("precondition: A -> B merge was refused: %v", err)
	}

	before := len(w3Events(t, st, "site-a", 1))

	// Second, contradictory merge: A -> C, while A is already merged into B.
	cID := c.TradeID
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: a.TradeID, SameAs: &cID}, w3Base+2*w3Hour, "default")
	if err == nil {
		t.Fatalf("Q9c: %q was already merged into %q, and a SECOND same_as amendment (into %q) was ACCEPTED — "+
			"trade_amend.go:644's guard did not fire. A standing merge must be cleared (the un-merge) before a "+
			"new one is authored, or the trade heading a muster group prints under silently churns", a.TradeID, b.TradeID, c.TradeID)
	}
	want := a.TradeID + " is already merged with " + b.TradeID + "; clear same_as (the un-merge) first"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Q9c: refusal text = %q, want it to contain %q — this is the sentence the officer reads", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("Q9c: the refused second merge appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// ---------------------------------------------------------------------------
// The remaining unreached AmendTrade refusal arms — no census plant named
// these; each has its own line-anchored mutant, authored here in plants.py's
// own shape (exact stripped text asserted, `if false && …` disables the
// guard) and reported in reports/witness/y7-trade.md.
// ---------------------------------------------------------------------------

// TestWitnessY7XUnknownTradeIDIsRefused witnesses trade_amend.go:584:
// `if tr == nil {`. Amending an id that is not a trade on this post must be
// refused (a *TradeAmendInputError, not a fact-domain refusal — the officer
// named a record this post cannot act on). Mutant: `if false && tr == nil {`
// — the nil *Trade falls through untouched and the first dereference of
// `tr.SameAs` at Arm 8 (line 644) nil-panics instead of refusing cleanly.
func TestWitnessY7XUnknownTradeIDIsRefused(t *testing.T) {
	st := w3NewStore(t)
	before := len(w3Events(t, st, "site-a", 1))

	name := "Scaffolding"
	_, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: "not-a-trade-id", Name: &name}, w3Base, "default")
	if err == nil {
		t.Fatalf("X: an amendment naming a trade id that is not on this post was ACCEPTED — trade_amend.go:584's guard did not fire")
	}
	const want = `"not-a-trade-id" is not a trade on this post`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XNameToEmptyIsRefused witnesses trade_amend.go:604:
// `if in.Name != nil && strings.TrimSpace(*in.Name) == "" {`. A trade always
// has a name (trade.name is TEXT NOT NULL); blanking it must be refused.
// Mutant: `if false && …` — a whitespace-only name is authored and folds
// straight into the row, leaving a trade heading with an empty label.
func TestWitnessY7XNameToEmptyIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := len(w3Events(t, st, "site-a", 1))

	blank := "   "
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Name: &blank}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("X: an amendment blanking %q's name (to whitespace) was ACCEPTED — trade_amend.go:604's guard did not fire", tr.TradeID)
	}
	const want = "a trade's name cannot be amended to empty"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XClearNameIsRefused witnesses trade_amend.go:609:
// `if cf == "name" {`. Clear:["name"] must be refused, mirroring the
// person/company rule. Mutant: `if false && …` — a trade's name is cleared
// straight into the nil branch, which trade_amend.go:146-157's setter turns
// into the empty string on a NOT NULL column.
func TestWitnessY7XClearNameIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := len(w3Events(t, st, "site-a", 1))

	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Clear: []string{"name"}}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("X: an amendment with Clear:[\"name\"] against %q was ACCEPTED — trade_amend.go:609's guard did not fire", tr.TradeID)
	}
	const want = "a trade's name cannot be cleared"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XClearOutOfSetFieldIsRefused witnesses trade_amend.go:616:
// `if !isTradeAmendableFact(cf) {`. Clearing a fact outside the amendable
// set is the ONLY reachable shape of "an out-of-set fact" (setFields()
// builds its map from the input struct's own fields, so a Set-side
// assertion can't name one). Mutant: `if false && …` — an arbitrary,
// unrecognised field name in Clear is silently accepted and ignored (no
// setter exists for it, so applyTradeAmendmentRow's `if set, ok :=
// tradeFactSetters[fact]; ok` at line 466 just skips it at fold time).
func TestWitnessY7XClearOutOfSetFieldIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := len(w3Events(t, st, "site-a", 1))

	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Clear: []string{"retired"}}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("X: an amendment with Clear:[\"retired\"] (not an amendable trade field) against %q was ACCEPTED — trade_amend.go:616's guard did not fire", tr.TradeID)
	}
	const want = `"retired" is not an amendable trade field`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XSetAndClearedSameFieldIsRefused witnesses trade_amend.go:620:
// `if v := set[cf]; v != nil {`. Asserting and clearing the same fact in one
// amendment is contradictory and must be refused. Mutant: `if false && …` —
// PhoneDay is both set to a value AND cleared in the same call; whichever
// the fold applies last wins silently, with no officer-visible complaint
// about the contradiction itself.
func TestWitnessY7XSetAndClearedSameFieldIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := len(w3Events(t, st, "site-a", 1))

	num := "555-0100"
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, PhoneDay: &num, Clear: []string{"phone_day"}}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("X: an amendment both setting AND clearing phone_day against %q was ACCEPTED — trade_amend.go:620's guard did not fire", tr.TradeID)
	}
	const want = `"phone_day" is both set and cleared in one amendment`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XSameAsTargetNotATradeIsRefused witnesses trade_amend.go:637:
// `if tt == nil {`. `same_as` joins TRADE records only (W5d-13's shape,
// trade-sided) — the kind-homogeneity check the id-kind-agnostic mechanism
// this replaced never had (P-3 §2 fact 5). Mutant: `if false && …` — a
// same_as target that names no trade record at all is authored anyway.
func TestWitnessY7XSameAsTargetNotATradeIsRefused(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := len(w3Events(t, st, "site-a", 1))

	notATrade := "not-a-trade-id"
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, SameAs: &notATrade}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("X: a same_as amendment naming %q, which is not a trade record, was ACCEPTED — trade_amend.go:637's guard did not fire", notATrade)
	}
	const want = `"not-a-trade-id" is not a trade record; same_as joins trade records only`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused amendment appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// TestWitnessY7XMergeCycleIsRefused witnesses trade_amend.go:660:
// `if next == in.TradeID {`. Closing a merge cycle back to the record being
// amended must be refused, walked over the COMPOSED resolution state.
// Mutant: `if false && …` — with C already merged into A, amending A to
// merge into C closes a two-node cycle (A -> C -> A) and is authored anyway.
// Distinct from Q9c: here tr.SameAs (A's own) is nil coming in, so Arm 8
// does not fire — only the cycle walk can catch this one.
func TestWitnessY7XMergeCycleIsRefused(t *testing.T) {
	st := w3NewStore(t)
	a, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateTrade(st, "site-a", 1, "Rigging", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}

	// C -> A: legitimate, and the precondition for the cycle under test.
	aID := a.TradeID
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: c.TradeID, SameAs: &aID}, w3Base+w3Hour, "default"); err != nil {
		t.Fatalf("precondition: C -> A merge was refused: %v", err)
	}

	before := len(w3Events(t, st, "site-a", 1))

	// A -> C would close the cycle A -> C -> A. A.SameAs is nil coming in
	// (Arm 8 does not fire), so only the cycle walk (Arm 9) can refuse this.
	cID := c.TradeID
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: a.TradeID, SameAs: &cID}, w3Base+2*w3Hour, "default")
	if err == nil {
		t.Fatalf("X: merging %q into %q, closing the cycle %s -> %s -> %s, was ACCEPTED — trade_amend.go:660's guard did not fire",
			a.TradeID, c.TradeID, a.TradeID, c.TradeID, a.TradeID)
	}
	want := "merging " + a.TradeID + " into " + c.TradeID + " would close a cycle"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("X: refusal text = %q, want it to contain %q", err.Error(), want)
	}
	if after := len(w3Events(t, st, "site-a", 1)); after != before {
		t.Errorf("X: the refused cyclic merge appended %d event(s) — a refusal must append NOTHING (W3-2)", after-before)
	}
}

// ---------------------------------------------------------------------------
// ERR-ARM: internal-fault wraps, not officer refusals. Fault-injected via
// `w8Exec` (internal/domain/w8_absences_test.go:543 — st.LocalTx, the
// sanctioned read/write path; already has DROP TABLE precedent in this
// package at w8_absences_test.go:487/500 and w8_deletions_test.go:463). y3-acks
// adjudicated this technique for the whole phase — see
// reports/witness/y3-faultseam.md. Table-level granularity only: two calls
// that depend on the SAME table inside ONE guarded block cannot be told
// apart this way. Two of the seven ERR-ARM sites in this file hit exactly
// that ceiling and are reported unwitnessed below rather than forced.
// ---------------------------------------------------------------------------

// TestWitnessY7ERR2FoldInsertFailureIsWrapped witnesses trade_amend.go:323:
// `if _, err := tx.Exec(tradeAmendmentInsertSQL, args...); err != nil {`
// inside applyTradeAmended, the FOLD's own INSERT into the trade_amendment
// side table. Called DIRECTLY (bypassing AmendTrade and Author entirely —
// applyTradeAmended is the projection's Apply callback, invoked in
// isolation the way store.insertAndFold would call it) so the fault is
// isolated to exactly this INSERT, with no earlier read of the same table
// to entangle it. Mutant: `if _, err := …; false && err != nil {` — the
// fold silently returns nil (success) even though nothing was written.
func TestWitnessY7ERR2FoldInsertFailureIsWrapped(t *testing.T) {
	st := w3NewStore(t)
	w8Exec(t, st, `DROP TABLE trade_amendment`)

	payload, err := json.Marshal(map[string]any{
		"type":     "trade_amended",
		"trade_id": "t-x",
		"name":     "New Name",
	})
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{SiteID: "site-a", NodeEpoch: 1, Seq: 1, Payload: payload}

	err = st.LocalTx(func(tx *sql.Tx) error {
		return applyTradeAmended(tx, env)
	})
	if err == nil {
		t.Fatalf("ERR2: applyTradeAmended's INSERT into a DROPPED trade_amendment table reported SUCCESS — trade_amend.go:323's guard did not fire")
	}
	const want = "domain: trade_amended fold: insert"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("ERR2: error = %q, want it to contain %q", err.Error(), want)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: the
	// "domain: trade_amended fold: insert" prefix alone is arm-blind — it
	// is a wrap this test's OWN INSERT adds regardless of which table
	// fails to exist. Pin the assertion to the table this fixture
	// guarantees is gone, so a future rung reading trade_amendment before
	// this INSERT can't turn this witness into a false green.
	const wantCause = "no such table: trade_amendment"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("ERR2: error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "ERR2", err.Error())
	}
}

// TestWitnessY7ERR3ReadTradeAmendmentsFailureIsWrapped witnesses
// trade_amend.go:442: `if err != nil { return nil, fmt.Errorf("domain: read
// trade amendments: %w", err) }` inside queryTradeAmendments — the shared
// decode loop under both readTradeAmendments and readTradeAmendmentsFor.
// Called via the whole-table read directly, not through AmendTrade or
// TradeByID, so it is not entangled with any earlier read of the same
// table. Mutant: `if false && err != nil {` — the query failure is
// swallowed and an empty, successful result is returned instead.
func TestWitnessY7ERR3ReadTradeAmendmentsFailureIsWrapped(t *testing.T) {
	st := w3NewStore(t)
	w8Exec(t, st, `DROP TABLE trade_amendment`)

	_, err := readTradeAmendments(st)
	if err == nil {
		t.Fatalf("ERR3: readTradeAmendments against a DROPPED trade_amendment table reported SUCCESS — trade_amend.go:442's guard did not fire")
	}
	const want = "domain: read trade amendments:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("ERR3: error = %q, want it to contain %q", err.Error(), want)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: this wrap
	// fires on ANY error from the inner scan loop, not specifically a
	// missing trade_amendment table — pin the assertion to the fault this
	// fixture guarantees so a future read added ahead of this one inside
	// queryTradeAmendments can't turn this witness into a false green.
	const wantCause = "no such table: trade_amendment"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("ERR3: error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	// [[reports/witness/u1b-domain.md finding 🟡3]] MEASURED: ERR4
	// (LoadTradeMergeMap) also reds under THIS plant (trade_amend.go:442),
	// since it calls readTradeAmendments internally — a linear chain, not
	// two independent kills of :442. ERR3 does NOT red under ERR4's own
	// plant (:509). Do not credit these as independent in a future census.

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "ERR3", err.Error())
	}
}

// TestWitnessY7ERR4LoadTradeMergeMapFailureIsWrapped witnesses
// trade_amend.go:509: `if err != nil { return nil, fmt.Errorf("domain: load
// trade merge map: %w", err) }` inside the EXPORTED LoadTradeMergeMap.
// Called directly (not via AmendTrade's Arm 9, which wraps a SEPARATE call
// to this same function at line 651 and cannot be isolated the same way —
// see ERR7 below), so this is its own, distinct call site. Mutant: `if
// false && err != nil {` — LoadTradeMergeMap returns an EMPTY alias map
// with a nil error instead of surfacing the read failure, which would make
// every trade in the store appear unmerged.
func TestWitnessY7ERR4LoadTradeMergeMapFailureIsWrapped(t *testing.T) {
	st := w3NewStore(t)
	w8Exec(t, st, `DROP TABLE trade_amendment`)

	_, err := LoadTradeMergeMap(st)
	if err == nil {
		t.Fatalf("ERR4: LoadTradeMergeMap against a DROPPED trade_amendment table reported SUCCESS — trade_amend.go:509's guard did not fire")
	}
	const want = "domain: load trade merge map:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("ERR4: error = %q, want it to contain %q", err.Error(), want)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: pin the
	// assertion to the fault this fixture guarantees (see ERR3's identical
	// note) rather than to the wrap text alone.
	const wantCause = "no such table: trade_amendment"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("ERR4: error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}
	// [[reports/witness/u1b-domain.md finding 🟡3]] MEASURED: this test
	// ALSO reds under ERR3's plant (trade_amend.go:442), since
	// LoadTradeMergeMap calls readTradeAmendments internally — a linear
	// chain, not two independent kills. This test's OWN plant (:509) does
	// NOT red ERR3. Do not credit these as independent in a future census.

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "ERR4", err.Error())
	}
}

// TestWitnessY7ERR5AmendTradeFirstLookupFailureIsWrapped witnesses
// trade_amend.go:581: `if err != nil { return store.Envelope{}, fmt.Errorf(
// "domain: amend trade: %w", err) }` — AmendTrade's OWN wrap of its first
// TradeByID(st, in.TradeID) call. The trade is created (and the `trade`
// table intact) up to that point, then the table is dropped so the very
// next TradeByID call fails with a genuine fault rather than a "no such
// row" not-found. Mutant: `if false && err != nil {` — the internal fault
// is swallowed and control falls to `if tr == nil`, which fires instead: an
// officer would read "%q is not a trade on this post" (TradeAmendInputError)
// for what is actually a broken store, a materially misleading message.
func TestWitnessY7ERR5AmendTradeFirstLookupFailureIsWrapped(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	w8Exec(t, st, `DROP TABLE trade`)

	name := "New Name"
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Name: &name}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("ERR5: AmendTrade against a DROPPED trade table reported SUCCESS — trade_amend.go:581's guard did not fire")
	}
	if _, ok := err.(*TradeAmendInputError); ok {
		t.Fatalf("ERR5: AmendTrade returned a *TradeAmendInputError (%q) for a DROPPED trade table — an internal "+
			"fault is being misreported to the officer as \"not a trade on this post\"; trade_amend.go:581's guard did not fire", err.Error())
	}
	const want = "domain: amend trade:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("ERR5: error = %q, want it to contain %q", err.Error(), want)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "ERR5", err.Error())
	}
}

// TestWitnessY7ERR9AmendTradeAuthorFailureIsWrapped witnesses
// trade_amend.go:687: `if err != nil { return store.Envelope{}, fmt.Errorf(
// "domain: amend trade: %w", err) }` — AmendTrade's wrap of the final
// st.Author(...) call. `event_log` is dropped AFTER the trade is created
// (so every earlier read in this call — TradeByID against `trade` and
// `trade_amendment`, neither of which touches `event_log` — stays intact),
// isolating the fault to Author's own INSERT. Mutant: `if false && err !=
// nil {` — AmendTrade returns a zero store.Envelope AND a nil error: the
// caller is told an event was authored when nothing was appended at all.
func TestWitnessY7ERR9AmendTradeAuthorFailureIsWrapped(t *testing.T) {
	st := w3NewStore(t)
	tr, err := CreateTrade(st, "site-a", 1, "Scaffolding", w3Base, "default")
	if err != nil {
		t.Fatal(err)
	}
	w8Exec(t, st, `DROP TABLE event_log`)

	name := "New Name"
	_, err = AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Name: &name}, w3Base+w3Hour, "default")
	if err == nil {
		t.Fatalf("ERR9: AmendTrade's Author() call against a DROPPED event_log table reported SUCCESS — trade_amend.go:687's guard did not fire")
	}
	const want = "domain: amend trade:"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("ERR9: error = %q, want it to contain %q", err.Error(), want)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED (PRODUCED): the
	// "domain: amend trade:" prefix is SHARED across at least five call
	// sites in AmendTrade (trade_amend.go:582/635/652/684/688) — dropping
	// `trade_amendment` instead of `event_log` here still passes this
	// prefix check (it fires ERR5's arm at :581 instead). Pin the assertion
	// to the table this fixture guarantees is gone.
	const wantCause = "no such table: event_log"
	if !strings.Contains(err.Error(), wantCause) {
		t.Errorf("ERR9: error = %q, want it to name the dropped table (%q)", err.Error(), wantCause)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "ERR9", err.Error())
	}
}

// witnessRequireRawRoot walks err through every Unwrap and fails if the
// innermost error still carries one of this module's own prefixes. A %w→%s
// swap at ANY layer leaves .Error() byte-identical but makes that layer the
// root, so this is the one assertion strings.Contains cannot fake.
// [[PCTW-SWEEP]] (2026-09-06).
func witnessRequireRawRoot(t *testing.T, tag string, err error) {
	t.Helper()
	root := err
	for {
		u := errors.Unwrap(root)
		if u == nil {
			break
		}
		root = u
	}
	for _, p := range []string{"domain: ", "store: ", "deadline: "} {
		if strings.HasPrefix(root.Error(), p) {
			t.Errorf("%s: the innermost error is %q — one of our own wrappers, so a %%w between it and the underlying fault has been lost (the full text is unchanged, so no Contains check can see this)", tag, root.Error())
			return
		}
	}
}
