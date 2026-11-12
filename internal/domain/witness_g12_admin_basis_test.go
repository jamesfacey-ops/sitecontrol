// witness_g12_admin_basis_test.go — [[G12]] (James's ruling, 2026-09-08):
// createTrade's comparison basis moved from BASE rows
// (dumpSection[Trade](st, "trade")) to the COMPOSED list (Trades()) —
// refdata.go, one line, applying to BOTH doors behind the private
// createTrade (CreateTrade the admin door, CreateTradeRecordedBy the gate
// escape and intake), because the uniqueness check lives once, inside it.
//
// The gate half was forced by the locked D-66/D-71 criterion (THE GATE
// NEVER REFUSES) — see g12_build_repro_test.go for its reproduction and
// reports/witness/G12-CREATE-DOOR-BASIS.md for the full trace.
//
// This file is the half James specifically asked for: the ADMIN door's
// semantics genuinely change under the same fix, in TWO directions, and
// nothing before this file asserted either the old or the new behaviour —
// so the change shipped untested. Both are pinned here, deliberately:
//
//  1. CreateTrade newly REFUSES a name that collides only at the COMPOSED
//     level (a name arrived at by an earlier rename) — it did not before,
//     because the base scan had no row under that name to find.
//  2. CreateTrade newly ALLOWS reuse of a name a rename FREED — the base
//     row's stale spelling no longer blocks it, symmetric to the property
//     witness_d82_rename_guard_test.go already proves for AmendTrade
//     (TestD82RenameOntoANameFreedByAnEarlierRenameIsAllowed).
//
// Explicitly OUT of scope (G3/G3b remain gated, unresolved by this file):
// whether the admin door OUGHT to use a different, stricter basis than the
// gate. This file tests what the ruled fix DOES, not whether a different
// fix would have been better.
package domain

import (
	"errors"
	"testing"

	"sitecontrol/internal/store"
)

// g12RenamedTrade seeds `from`, renames it onto the FREE name `to`, and
// returns the (unchanged) TradeID plus the pre-rename watermark for undo
// checks.
func g12RenamedTrade(t *testing.T, st *store.Store, from, to string) (tradeID string) {
	t.Helper()
	tr, err := CreateTrade(st, "site-a", 1, from, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendTrade(st, "site-a", 1, TradeAmendment{TradeID: tr.TradeID, Name: &to}, 0, "default"); err != nil {
		t.Fatalf("AmendTrade(%s -> %s) onto a FREE name = %v, want success", from, to, err)
	}
	return tr.TradeID
}

// TestG12AdminCreateNowRefusesAComposedNameCollision is direction 1: before
// this fix, CreateTrade compared only against base rows, so a name that
// exists ONLY at the composed level (arrived at by renaming a different
// trade) collided with nothing and a second identical heading was minted —
// reproduced in g12_build_repro_test.go
// (TestG12BuildReproAdminCreateMintsASecondIdenticalHeading). This test
// pins the fixed behaviour going forward: the refusal is TYPED
// (TradeCreateRefusal), the watermark does not move, and exactly one
// Pipefitter row survives.
func TestG12AdminCreateNowRefusesAComposedNameCollision(t *testing.T) {
	st := newDomainTestStore(t)
	g12RenamedTrade(t, st, "Welder", "Pipefitter")

	wmBefore, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateTrade(st, "site-a", 1, "Pipefitter", 0, "default")
	if err == nil {
		t.Fatal("CreateTrade(\"Pipefitter\") after Welder->Pipefitter = nil error, want a TradeCreateRefusal — this is the exact defect G12 fixes")
	}
	var refusal *TradeCreateRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("CreateTrade(\"Pipefitter\") after the rename returned %v, want a *TradeCreateRefusal (typed, per D-76's errors.As contract)", err)
	}
	if refusal.Existing != "Pipefitter" {
		t.Errorf("refusal.Existing = %q, want %q", refusal.Existing, "Pipefitter")
	}

	wmAfter, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wmAfter != wmBefore {
		t.Errorf("CreateTrade(\"Pipefitter\"): watermark advanced %d -> %d on a refusal", wmBefore, wmAfter)
	}

	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	for _, tr := range trades {
		if tr.Name == "Pipefitter" {
			hits++
		}
	}
	if hits != 1 {
		t.Errorf("Pipefitter rows after the refused create = %d, want exactly 1", hits)
	}
}

// TestG12AdminCreateNowAllowsReuseOfANameFreedByARename is direction 2, the
// other side of the same fix: before it, the base row's stale "Welder"
// spelling blocked reuse forever even though nothing on the composed list —
// the muster the officers actually read — holds that name any more. This
// is symmetric to the property witness_d82_rename_guard_test.go already
// proves for AmendTrade (a name a rename frees is takeable); this test
// pins it for CreateTrade too. Not previously asserted anywhere — the
// g12-basis-cost measurement found this direction genuinely untested.
func TestG12AdminCreateNowAllowsReuseOfANameFreedByARename(t *testing.T) {
	st := newDomainTestStore(t)
	originalID := g12RenamedTrade(t, st, "Welder", "Pipefitter")

	reused, err := CreateTrade(st, "site-a", 1, "Welder", 0, "default")
	if err != nil {
		t.Fatalf("CreateTrade(\"Welder\") after Welder->Pipefitter froze the name = %v, want success — the rename freed it at the composed level", err)
	}
	if reused.TradeID == originalID {
		t.Fatalf("CreateTrade(\"Welder\") returned the ORIGINAL trade id %q, want a freshly minted one distinct from the renamed trade", originalID)
	}

	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var welderHits, pipefitterHits int
	for _, tr := range trades {
		switch tr.Name {
		case "Welder":
			welderHits++
		case "Pipefitter":
			pipefitterHits++
		}
	}
	if welderHits != 1 {
		t.Errorf("Welder rows after reuse = %d, want exactly 1 (the freshly minted one)", welderHits)
	}
	if pipefitterHits != 1 {
		t.Errorf("Pipefitter rows after reuse = %d, want exactly 1 (the renamed original, untouched)", pipefitterHits)
	}
}
