// witness_d82_rename_guard_tester_test.go — the INDEPENDENT tester pass over
// [[D-82]] (2026-09-05): "renaming a trade onto a listed name is REFUSED —
// refuse, never merge". Written by tester-go against cb7f775, not by the
// builder of trade_amend.go's Arm 10, and every witness here was watched RED
// under a one-line mutant of that arm before it was trusted GREEN (the
// mutant, the command and the red line are in
// reports/witness/TESTER-d82.md §6).
//
// What this file adds to the builder's witness_d82_rename_guard_test.go:
//
//   - PRE-APPEND asserted as "no envelope exists", not as "the watermark did
//     not move" alone: the whole authored stream is compared envelope-by-
//     envelope (Hash) across the refusal, and no trade_amended payload is
//     anywhere in it.
//   - TWO POSTS: a refused rename at post A ships nothing to post B, the two
//     composed lists stay identical, and the guard at B reads the rows that
//     REPLICATED in — a refusal that authored anything would silently
//     diverge the two logs (W3-2's m6, mint-then-refuse).
//   - FOLD PARITY: every spelling the create door refuses, the rename door
//     refuses, and both name the same holder row.
//   - THE SELF CASE: a trade renamed to a case variant of ITS OWN name
//     records — without this an officer could never fix a capitalisation.
//   - COMPOSED-vs-RAW, both halves in one place, and the refusal's class
//     membership asserted BY TYPE (errors.As), never by .Error() text.
//
// (C1) Every list compared here is SORTED before comparison — nothing
// depends on the order Trades() happens to return.
package domain

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// d82tCreate seeds one trade at site-a/epoch-1 and fails the test on refusal.
func d82tCreate(t *testing.T, st *store.Store, name string) Trade {
	t.Helper()
	tr, err := CreateTrade(st, "site-a", 1, name, 0, "default")
	if err != nil {
		t.Fatalf("seed CreateTrade(%q) = %v", name, err)
	}
	return tr
}

// d82tRenameAt asserts trade `id`'s name to `name` at post (siteID, 1)
// through the real authoring edge and returns the error, unexamined.
func d82tRenameAt(st *store.Store, siteID, id, name string) error {
	_, err := AmendTrade(st, siteID, 1, TradeAmendment{TradeID: id, Name: &name}, 0, "default")
	return err
}

// d82tStream is the FULL authored stream of (siteID, epoch 1) — every
// envelope, in seq order. The refusal witnesses compare this whole slice,
// not a watermark.
func d82tStream(t *testing.T, st *store.Store, siteID string) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// d82tWatermark is (siteID, 1)'s applied watermark.
func d82tWatermark(t *testing.T, st *store.Store, siteID string) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// d82tListing is the COMPOSED trade list as sorted "id=name" strings — the
// muster-heading view, in a fixed order (C1).
func d82tListing(t *testing.T, st *store.Store) []string {
	t.Helper()
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(trades))
	for _, tr := range trades {
		out = append(out, tr.TradeID+"="+tr.Name)
	}
	sort.Strings(out)
	return out
}

// d82tRowsNamed returns the sorted ids of every composed trade named
// EXACTLY `name`.
func d82tRowsNamed(t *testing.T, st *store.Store, name string) []string {
	t.Helper()
	trades, err := Trades(st)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tr := range trades {
		if tr.Name == name {
			ids = append(ids, tr.TradeID)
		}
	}
	sort.Strings(ids)
	return ids
}

// d82tSync replays (siteID, 1)'s stream from src into dst and returns how
// many envelopes dst actually APPLIED (a replay of an already-applied prefix
// applies zero).
func d82tSync(t *testing.T, src, dst *store.Store, siteID string) int {
	t.Helper()
	applied := 0
	for _, ev := range d82tStream(t, src, siteID) {
		ok, err := dst.Apply(ev)
		if err != nil {
			t.Fatalf("replicating %v into the peer: %v", ev.Key(), err)
		}
		if ok {
			applied++
		}
	}
	return applied
}

// d82tEqual compares two sorted string slices.
func d82tEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// d82tMustBeRenameRefusal fails the test unless err is a *TradeRenameRefusal
// and returns it.
func d82tMustBeRenameRefusal(t *testing.T, what string, err error) *TradeRenameRefusal {
	t.Helper()
	var refusal *TradeRenameRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("%s = %v, want a *TradeRenameRefusal (D-82)", what, err)
	}
	return refusal
}

// --- PRE-APPEND: no envelope, not merely no watermark movement --------------

// TestD82TesterARefusedRenameAppendsNoEnvelopeAtAll: after the refusal the
// authored stream is the SAME SLICE OF ENVELOPES it was before — same
// length, same seq, same Hash at every position — and no envelope in it
// carries a trade_amended payload. A refusal that had already called Author
// (W3-2's m6, mint-then-refuse) leaves the officer a 409 and the log an
// event, and the two posts' logs then differ by an envelope nobody meant.
func TestD82TesterARefusedRenameAppendsNoEnvelopeAtAll(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter := d82tCreate(t, st, "Pipefitter")
	catering := d82tCreate(t, st, "Catering")
	before := d82tStream(t, st, "site-a")
	wmBefore := d82tWatermark(t, st, "site-a")

	err := d82tRenameAt(st, "site-a", catering.TradeID, "Pipefitter")
	refusal := d82tMustBeRenameRefusal(t, `AmendTrade(Catering → "Pipefitter") with Pipefitter listed`, err)
	if refusal.Existing.TradeID != pipefitter.TradeID {
		t.Errorf("refusal.Existing.TradeID = %q, want the Pipefitter row %q", refusal.Existing.TradeID, pipefitter.TradeID)
	}

	after := d82tStream(t, st, "site-a")
	if len(after) != len(before) {
		t.Fatalf("the authored stream grew across a refusal: %d → %d envelopes — the refusal is not PRE-APPEND (W3-2 m6)", len(before), len(after))
	}
	for i := range before {
		if before[i].Seq != after[i].Seq || before[i].Hash() != after[i].Hash() {
			t.Errorf("envelope %d changed across the refusal: seq %d/%d hash %s/%s", i, before[i].Seq, after[i].Seq, before[i].Hash(), after[i].Hash())
		}
	}
	for _, ev := range after {
		if strings.Contains(string(ev.Payload), `"trade_amended"`) {
			t.Errorf("a trade_amended envelope exists at %v after a REFUSED rename: %s", ev.Key(), ev.Payload)
		}
	}
	if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore {
		t.Errorf("watermark moved %d → %d across a refusal", wmBefore, wmAfter)
	}
	if ids := d82tRowsNamed(t, st, "Pipefitter"); !d82tEqual(ids, []string{pipefitter.TradeID}) {
		t.Errorf("rows named Pipefitter = %v, want exactly [%s] — two would be two identical muster headings", ids, pipefitter.TradeID)
	}
}

// TestD82TesterARefusedRenameShipsNothingToThePeerAndTheGuardReadsReplicatedRows
// is the two-post half. Post A seeds Pipefitter and Catering; post B
// receives them. A's refused rename ships ZERO envelopes to B, the two
// composed lists stay identical, and B's own guard refuses Catering →
// "pipefitter" against the Pipefitter row that only ever REPLICATED in
// (the guard reads the composed list at the post it runs on, and that list
// includes the peer's rows). Then a legitimate rename at B ships to A and
// the lists agree again — so the refusal is what kept them equal, not an
// accident of nothing happening.
func TestD82TesterARefusedRenameShipsNothingToThePeerAndTheGuardReadsReplicatedRows(t *testing.T) {
	a, b := newDomainTestStore(t), newDomainTestStore(t)
	pipefitter := d82tCreate(t, a, "Pipefitter")
	catering := d82tCreate(t, a, "Catering")
	if n := d82tSync(t, a, b, "site-a"); n != 2 {
		t.Fatalf("precondition: first sync applied %d envelopes at B, want 2", n)
	}
	if !d82tEqual(d82tListing(t, a), d82tListing(t, b)) {
		t.Fatalf("precondition: the two posts' lists differ after sync: A %v B %v", d82tListing(t, a), d82tListing(t, b))
	}

	// A refuses.
	d82tMustBeRenameRefusal(t, `post A: AmendTrade(Catering → "Pipefitter")`, d82tRenameAt(a, "site-a", catering.TradeID, "Pipefitter"))
	if n := d82tSync(t, a, b, "site-a"); n != 0 {
		t.Errorf("after A's REFUSED rename, a sync applied %d new envelope(s) at B, want 0 — the refusal authored something and the two logs now differ by it", n)
	}
	if la, lb := d82tListing(t, a), d82tListing(t, b); !d82tEqual(la, lb) {
		t.Errorf("after A's refused rename the composed lists differ: A %v B %v", la, lb)
	}
	if wa, wb := d82tWatermark(t, a, "site-a"), d82tWatermark(t, b, "site-a"); wa != wb || wa != 2 {
		t.Errorf("site-a watermark at A = %d, at B = %d, want both 2 (two creates, nothing else)", wa, wb)
	}

	// B refuses too, against the row that replicated in.
	refusal := d82tMustBeRenameRefusal(t, `post B: AmendTrade(Catering → "pipefitter")`, d82tRenameAt(b, "site-b", catering.TradeID, "pipefitter"))
	if refusal.Existing.TradeID != pipefitter.TradeID || refusal.Existing.Name != "Pipefitter" {
		t.Errorf("B's refusal.Existing = %q %q, want the replicated Pipefitter row %q under its on-list spelling", refusal.Existing.TradeID, refusal.Existing.Name, pipefitter.TradeID)
	}
	if wb := d82tWatermark(t, b, "site-b"); wb != 0 {
		t.Errorf("B's OWN stream (site-b) watermark = %d after a refusal, want 0 — B authored nothing", wb)
	}

	// A legitimate rename at B does ship, and the lists agree again.
	if err := d82tRenameAt(b, "site-b", catering.TradeID, "Rigging"); err != nil {
		t.Fatalf(`post B: AmendTrade(Catering → "Rigging") = %v, want recorded — a free name`, err)
	}
	if n := d82tSync(t, b, a, "site-b"); n != 1 {
		t.Errorf("B's legitimate rename shipped %d envelope(s) to A, want exactly 1", n)
	}
	if la, lb := d82tListing(t, a), d82tListing(t, b); !d82tEqual(la, lb) {
		t.Errorf("after B's rename replicated, the lists differ: A %v B %v", la, lb)
	}
	if ids := d82tRowsNamed(t, a, "Rigging"); !d82tEqual(ids, []string{catering.TradeID}) {
		t.Errorf("rows named Rigging at A = %v, want [%s]", ids, catering.TradeID)
	}
}

// --- the fold, at both doors, and the self case ------------------------------

// TestD82TesterTheRenameDoorRefusesEverySpellingTheCreateDoorRefuses: D-76's
// fold (case + leading/trailing space) applied at the rename door yields the
// same verdict and the same holder as the create door, spelling by
// spelling; a name that differs by more than the fold ("Welders") records.
func TestD82TesterTheRenameDoorRefusesEverySpellingTheCreateDoorRefuses(t *testing.T) {
	st := newDomainTestStore(t)
	welder := d82tCreate(t, st, "Welder")
	catering := d82tCreate(t, st, "Catering")
	for _, spelling := range []string{"Welder", "welder", "  WELDER  ", "Welder\t", "wElDeR"} {
		wmBefore := d82tWatermark(t, st, "site-a")

		_, cerr := CreateTrade(st, "site-a", 1, spelling, 0, "default")
		var create *TradeCreateRefusal
		if !errors.As(cerr, &create) {
			t.Errorf("CreateTrade(%q) with Welder listed = %v, want a *TradeCreateRefusal (D-76, the create door)", spelling, cerr)
		} else if create.Existing != "Welder" {
			t.Errorf("CreateTrade(%q): refusal.Existing = %q, want the on-list spelling \"Welder\"", spelling, create.Existing)
		}

		rerr := d82tRenameAt(st, "site-a", catering.TradeID, spelling)
		var rename *TradeRenameRefusal
		if !errors.As(rerr, &rename) {
			t.Errorf("AmendTrade(Catering → %q) with Welder listed = %v, want a *TradeRenameRefusal (D-82, the rename door, same fold)", spelling, rerr)
		} else if rename.Existing.TradeID != welder.TradeID || rename.Existing.Name != "Welder" || rename.Typed != spelling {
			t.Errorf("AmendTrade(Catering → %q): refusal (Existing %q %q, Typed %q), want (%q \"Welder\" %q)", spelling, rename.Existing.TradeID, rename.Existing.Name, rename.Typed, welder.TradeID, spelling)
		}

		if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore {
			t.Errorf("spelling %q: watermark moved %d → %d across two refusals", spelling, wmBefore, wmAfter)
		}
	}
	// Outside the fold: a different name, not a variant.
	wmBefore := d82tWatermark(t, st, "site-a")
	if err := d82tRenameAt(st, "site-a", catering.TradeID, "Welders"); err != nil {
		t.Fatalf(`AmendTrade(Catering → "Welders") = %v, want recorded — "Welders" is not a case or padding variant of "Welder"`, err)
	}
	if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore+1 {
		t.Errorf("watermark %d → %d, want exactly one authored event", wmBefore, wmAfter)
	}
	if ids := d82tRowsNamed(t, st, "Welder"); !d82tEqual(ids, []string{welder.TradeID}) {
		t.Errorf("rows named Welder = %v, want [%s] untouched", ids, welder.TradeID)
	}
}

// TestD82TesterATradeMayBeRenamedToACaseVariantOfItsOwnName is the SELF
// case the arm's `other.TradeID == in.TradeID` skip exists for. "welder" →
// "Welder" is the capitalisation fix D-76 made wanted; under the fold the
// trade "holds" its own case variants, and a guard that compared a trade
// against itself would refuse the officer's only way to fix a heading.
func TestD82TesterATradeMayBeRenamedToACaseVariantOfItsOwnName(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter := d82tCreate(t, st, "Pipefitter")
	lower := d82tCreate(t, st, "welder")

	for _, step := range []struct{ to, composed string }{
		{"Welder", "Welder"}, // the capitalisation fix
		{"WELDER", "WELDER"}, // another case variant of its own name
		{"Welder", "Welder"}, // and back — still its own
	} {
		wmBefore := d82tWatermark(t, st, "site-a")
		if err := d82tRenameAt(st, "site-a", lower.TradeID, step.to); err != nil {
			t.Fatalf("AmendTrade(%s → %q), a case variant of the trade's OWN name, = %v, want recorded — an officer must be able to fix a capitalisation", lower.TradeID, step.to, err)
		}
		if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore+1 {
			t.Errorf("%q: watermark %d → %d, want exactly one authored event", step.to, wmBefore, wmAfter)
		}
		got, err := TradeByID(st, lower.TradeID)
		if err != nil || got == nil {
			t.Fatal(err)
		}
		if got.Name != step.composed {
			t.Errorf("composed name after → %q = %q, want %q", step.to, got.Name, step.composed)
		}
	}
	// A padding variant of its own name is also not a collision with itself
	// (the arm skips self before it folds). What the composed name then
	// reads is the edge's pre-existing storage rule, not D-82's — logged,
	// not asserted.
	if err := d82tRenameAt(st, "site-a", lower.TradeID, "  Welder  "); err != nil {
		t.Errorf(`AmendTrade(welder → "  Welder  "), a padding variant of its OWN name, = %v, want not refused`, err)
	} else if got, _ := TradeByID(st, lower.TradeID); got != nil {
		t.Logf("MEASURED (not asserted): composed name after the padded self-rename = %q", got.Name)
	}
	// The OTHER trade is still exactly itself, and the self case did not
	// loosen the guard against it.
	d82tMustBeRenameRefusal(t, `AmendTrade(welder → "Pipefitter")`, d82tRenameAt(st, "site-a", lower.TradeID, "Pipefitter"))
	if ids := d82tRowsNamed(t, st, "Pipefitter"); !d82tEqual(ids, []string{pipefitter.TradeID}) {
		t.Errorf("rows named Pipefitter = %v, want [%s]", ids, pipefitter.TradeID)
	}
}

// --- COMPOSED, not RAW: both halves ------------------------------------------

// TestD82TesterTheGuardReadsTheComposedListBothHalves: (a) a name that
// exists ONLY by rename is held — Welder → Pipefitter, then Catering →
// Pipefitter is refused naming the Welder row; (b) a name FREED by that
// rename is takeable — Catering → Welder records. A guard over base rows
// (createTrade's read) passes (a) and refuses (b): it would print two
// Pipefitter headings and refuse a free name in the name of a trade that no
// longer holds it.
func TestD82TesterTheGuardReadsTheComposedListBothHalves(t *testing.T) {
	st := newDomainTestStore(t)
	welder := d82tCreate(t, st, "Welder")
	catering := d82tCreate(t, st, "Catering")
	if err := d82tRenameAt(st, "site-a", welder.TradeID, "Pipefitter"); err != nil {
		t.Fatalf(`setup: AmendTrade(Welder → "Pipefitter") = %v`, err)
	}

	// (a) held by rename only — no base row is named Pipefitter. Not fatal:
	// half (b) below must run in the same test so a raw-row guard reds BOTH
	// ways in one output.
	aErr := d82tRenameAt(st, "site-a", catering.TradeID, "Pipefitter")
	var refusal *TradeRenameRefusal
	if !errors.As(aErr, &refusal) {
		t.Errorf(`(a) AmendTrade(Catering → "Pipefitter") after Welder was RENAMED to Pipefitter = %v, want a *TradeRenameRefusal — the holder exists only on the composed list; the guard is reading base rows`, aErr)
	} else if refusal.Existing.TradeID != welder.TradeID || refusal.Existing.Name != "Pipefitter" {
		t.Errorf("(a) refusal.Existing = %q %q, want the renamed Welder row %q as \"Pipefitter\"", refusal.Existing.TradeID, refusal.Existing.Name, welder.TradeID)
	}
	if ids := d82tRowsNamed(t, st, "Pipefitter"); !d82tEqual(ids, []string{welder.TradeID}) {
		t.Errorf("(a) rows named Pipefitter = %v, want [%s]", ids, welder.TradeID)
	}

	// (b) freed by rename — the base row still says Welder, the list does not.
	wmBefore := d82tWatermark(t, st, "site-a")
	if err := d82tRenameAt(st, "site-a", catering.TradeID, "Welder"); err != nil {
		t.Fatalf(`(b) AmendTrade(Catering → "Welder") after Welder was renamed away = %v, want recorded — the guard over-refuses: it is reading base rows, not the list the muster prints`, err)
	}
	if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore+1 {
		t.Errorf("(b) watermark %d → %d, want exactly one authored event", wmBefore, wmAfter)
	}
	if ids := d82tRowsNamed(t, st, "Welder"); !d82tEqual(ids, []string{catering.TradeID}) {
		t.Errorf("(b) rows named Welder = %v, want [%s] (Catering under its new name)", ids, catering.TradeID)
	}
}

// --- the class, by type ---------------------------------------------------------

// TestD82TesterTheRefusalIsInTheFactDomainClassByTypeAndCarriesTheHolderAsData:
// errors.As reaches a *TradeAmendRefusal through Unwrap (web/trade.go's 409
// arm matches that type and nothing else), its Reason IS the rename
// sentence, the refusal is NOT a *TradeAmendInputError, and the holder is
// read as Existing (a Trade), never parsed out of the text. A lost Unwrap
// leaves .Error() byte-identical and turns the officer's 409 into a 500 —
// only a by-type assertion can see that.
func TestD82TesterTheRefusalIsInTheFactDomainClassByTypeAndCarriesTheHolderAsData(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter := d82tCreate(t, st, "Pipefitter")
	catering := d82tCreate(t, st, "Catering")
	err := d82tRenameAt(st, "site-a", catering.TradeID, "pipefitter")
	rename := d82tMustBeRenameRefusal(t, `AmendTrade(Catering → "pipefitter")`, err)

	var amend *TradeAmendRefusal
	if !errors.As(err, &amend) {
		t.Fatalf("errors.As(err, **TradeAmendRefusal) = false — the rename refusal is outside the fact-domain class BY TYPE; web/trade.go's partition answers 500 for it. err.Error() = %q", err.Error())
	}
	if amend.Reason != rename.Reason() {
		t.Errorf("unwrapped TradeAmendRefusal.Reason = %q, want the rename refusal's own sentence %q", amend.Reason, rename.Reason())
	}
	var input *TradeAmendInputError
	if errors.As(err, &input) {
		t.Errorf("errors.As(err, **TradeAmendInputError) = true — a collision is not a mistyped id")
	}
	// The holder AS DATA.
	if rename.Existing.TradeID != pipefitter.TradeID || rename.Existing.Name != "Pipefitter" || rename.Renamed.TradeID != catering.TradeID || rename.Typed != "pipefitter" {
		t.Errorf("refusal data = (Existing %q %q, Renamed %q, Typed %q), want (%q \"Pipefitter\", %q, \"pipefitter\")", rename.Existing.TradeID, rename.Existing.Name, rename.Renamed.TradeID, rename.Typed, pipefitter.TradeID, catering.TradeID)
	}
	// And the sentence names what the data says (informational; the data is
	// the contract).
	if !strings.Contains(rename.Reason(), pipefitter.TradeID) || !strings.Contains(rename.Reason(), `"Pipefitter"`) {
		t.Errorf("Reason() does not name the holder's id and on-list spelling: %q", rename.Reason())
	}
}

// TestD82TesterArmOrderAnEmptyNameIsArmTwoNotACollision: Arm 10 sits after
// Arm 2 — a blank name with Pipefitter listed is the name-to-empty refusal
// (a plain *TradeAmendRefusal), never a *TradeRenameRefusal; and nothing is
// recorded either way.
func TestD82TesterArmOrderAnEmptyNameIsArmTwoNotACollision(t *testing.T) {
	st := newDomainTestStore(t)
	d82tCreate(t, st, "Pipefitter")
	catering := d82tCreate(t, st, "Catering")
	wmBefore := d82tWatermark(t, st, "site-a")
	err := d82tRenameAt(st, "site-a", catering.TradeID, "   ")
	var rename *TradeRenameRefusal
	if errors.As(err, &rename) {
		t.Errorf(`AmendTrade(Catering → "   ") = a *TradeRenameRefusal (%v) — a blank name is Arm 2's refusal, not a collision`, err)
	}
	var amend *TradeAmendRefusal
	if !errors.As(err, &amend) || amend.Reason != "a trade's name cannot be amended to empty" {
		t.Errorf(`AmendTrade(Catering → "   ") = %v, want the Arm 2 refusal "a trade's name cannot be amended to empty"`, err)
	}
	if wmAfter := d82tWatermark(t, st, "site-a"); wmAfter != wmBefore {
		t.Errorf("watermark moved %d → %d", wmBefore, wmAfter)
	}
}

// --- [[D-66]]: the gate never refuses ---------------------------------------------

// TestD82TesterTheGateEscapeStillResolvesAndStillMintsAfterARefusedRename:
// after Catering → Pipefitter was refused, the add-on-demand escape typed
// with "PIPEFITTER" (a fold variant) resolves to the Pipefitter row and
// mints nothing; typed with "Rigging" (unlisted) it mints exactly one trade;
// the check-in itself is not refused. The escape's own path
// (checkin.go resolveOrCreateTradeByName → CreateTradeRecordedBy) never
// calls AmendTrade, so Arm 10 is not on it — this drives that path rather
// than reading it.
func TestD82TesterTheGateEscapeStillResolvesAndStillMintsAfterARefusedRename(t *testing.T) {
	st := newDomainTestStore(t)
	pipefitter := d82tCreate(t, st, "Pipefitter")
	catering := d82tCreate(t, st, "Catering")
	d82tMustBeRenameRefusal(t, `AmendTrade(Catering → "Pipefitter")`, d82tRenameAt(st, "site-a", catering.TradeID, "Pipefitter"))
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := d82tListing(t, st)

	_, _, err = GateCheckIn(st, "site-a", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-82T"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
		Occupants: []PersonSlot{
			{NewName: "Pat Existing", NewTrade: "PIPEFITTER"},
			{NewName: "Robin New", NewTrade: "Rigging"},
		},
	}, 0, "default")
	if err != nil {
		t.Fatalf("D-66: the gate REFUSED after a refused rename: %v", err)
	}
	after := d82tListing(t, st)
	if len(after) != len(before)+1 {
		t.Errorf("trade list grew by %d, want exactly 1 (Rigging minted once, PIPEFITTER resolved to the existing row): before %v after %v", len(after)-len(before), before, after)
	}
	if ids := d82tRowsNamed(t, st, "Pipefitter"); !d82tEqual(ids, []string{pipefitter.TradeID}) {
		t.Errorf("rows named Pipefitter after the check-in = %v, want [%s]", ids, pipefitter.TradeID)
	}
	if ids := d82tRowsNamed(t, st, "Rigging"); len(ids) != 1 {
		t.Errorf("rows named Rigging after the check-in = %v, want exactly one", ids)
	}
	persons, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range persons {
		if p.Name == "Pat Existing" && (p.TradeID == nil || *p.TradeID != pipefitter.TradeID) {
			t.Errorf("Pat Existing's trade = %s, want the existing Pipefitter row %q", d76Deref(p.TradeID), pipefitter.TradeID)
		}
	}
}
