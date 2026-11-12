// w9b_unbind_test.go — W9b-1's fold-and-composition halves (F4 §12 T12,
// T12b, T12c): the un-bind produces THREE outcomes, order-independently, and
// the middle one has TWO fillers.
//
//	⟨1⟩ REOPENS                                    TestW9b1RetractionReopensTheMisBoundTrip
//	⟨2⟩(a) STAYS arrived — surviving reference     TestW9b1DoublyReferencedTripStaysArrivedAndTheBindingMoves
//	⟨2⟩(b) STAYS arrived — Pass-2 correlation      TestW9b1CorrelationRebindsAndTheTripStaysArrived,
//	                                                TestW9b1CorrelationRebindsWhenTheCorrelatingArrivalCROSSES
//	⟨3⟩ READS cancelled                            TestW9b1CancelledTripReadsCancelledAfterRetraction
//
// ⚠ WHY THE MIDDLE OUTCOME IS THE POINT OF THIS FILE. W9b-1's previous text
// described **its own mutant**: read plainly it said "on retraction, set open
// unless cancelled", which IS m21, the unconditional reopen. The re-locked
// text is three outcomes with two fillers under ⟨2⟩, and the two fillers have
// SEPARATE mutants — m21 (unconditional reopen) dies on either filler, but
// **m19, the reference-only carve-out, is GREEN on ⟨2⟩(a)** (a surviving
// reference is there to find) and dies only on ⟨2⟩(b). Two fillers, two
// fixtures, two mutants. A single "stays arrived" test here would recreate
// the defect this sub-chunk exists not to recreate.
//
// The OVER-correction is pinned too: without T12b's tail, a build that never
// reopens anything satisfies ⟨2⟩. The tail is asserted in the same test, in
// the same fixture, as a POSITIVE control.
//
// WHAT IS NOT HERE, and where it is instead. W9b-1's alert, board-row, peer
// and muster halves need `internal/deadline` and `httptest`, and
// `internal/domain` cannot import `internal/deadline` (deadline imports
// domain). They live in `internal/web/w9b_reopen_surfaces_test.go`. The
// domain-observable proxy for "the expectation and board row are BACK" is
// `OpenExpectations`, which IS the shared input of the deadline scanner, the
// inbound console manifest and the /trips board rows — asserted here as well
// as there, because a state test that stopped at `trip_state.state` would be
// green on a build that reopened the row and left the expectation closed.
//
// KILLER MUTANTS: m14 (the no-op un-bind), m14b (apply-time validation
// dropping a dangling retraction), m19 (the reference-only carve-out), m21
// (the unconditional reopen), and the state-machine shortcut ⟨3⟩ kills.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// =========================================================================
// W9b-1 ⟨1⟩ — REOPENS (T12)
// =========================================================================

// TestW9b1RetractionReopensTheMisBoundTrip is ⟨1⟩ in full apart from the
// alert and muster halves: the mis-bound trip returns to **open**, its
// deadline expectation is BACK, the arrival takes the ArrivalRefRetracted
// disposition, and the `visit` table is byte-identical throughout.
//
// KILLER MUTANT: m14 — the no-op un-bind (the event ships, folds, and Pass 1
// still binds), which is the cheapest possible "fix" for verdict C-2.
//
// The expectation is asserted at THREE points — present before the mis-click,
// absent after it, present again after the retraction — so "the expectation
// is back" cannot be true because it was never gone.
func TestW9b1RetractionReopensTheMisBoundTrip(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-1", "occ-1")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-1", []string{"occ-1"}, 240)

	if !w9bHasExpectation(t, st, "site-b", x.TripID) {
		t.Fatalf("PRECONDITION: trip %s has no open deadline expectation at site-b before the mis-click — the fixture never had anything to silence", x.TripID)
	}

	a1 := w9bArriveExpected(t, st, "site-b", 1, x.TripID)
	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("PRECONDITION: after the mis-click trip %s is %q, want %q", x.TripID, got, TripStateArrived)
	}
	if w9bHasExpectation(t, st, "site-b", x.TripID) {
		t.Fatalf("PRECONDITION: the mis-click did not silence trip %s's expectation at site-b — the gap W9b exists to close is not present in this fixture", x.TripID)
	}

	visitsBefore := w9bSection(t, st, "visit")

	// The retraction is authored at the ORIGIN — F4 §3.6: "either post (the
	// mistake may be discovered at the origin — 'our truck is still out')".
	w9bApplyNext(t, st, "site-a", 1, w9bBase+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))

	// --- ⟨1⟩: the trip returns to OPEN --------------------------------
	if got := w9bTripState(t, st, x.TripID); got != TripStateOpen {
		t.Fatalf("W9b-1 ⟨1⟩ (m14 — the no-op un-bind): after `%s` on arrival %s, trip %s reads %q, want %q.\n"+
			"F4 §3.6: Pass 1 SKIPS any arrival whose id appears in the retraction set, so the trip it referenced reverts in Pass 3 to what the surviving raw facts say. "+
			"Nothing else here binds %s, so that is `open`",
			w9bEvtRefRetracted, a1.ArrivalID, x.TripID, got, TripStateOpen, x.TripID)
	}

	// --- ⟨1⟩: the deadline expectation and board row are BACK ----------
	if !w9bHasExpectation(t, st, "site-b", x.TripID) {
		t.Fatalf("W9b-1 ⟨1⟩: trip %s reopened but site-b holds no open deadline expectation for it. "+
			"`OpenExpectations` is the shared input of the deadline scanner, the inbound console manifest and the /trips board rows (F4 §9.1), "+
			"so a reopen that does not restore it restores nothing an officer can see", x.TripID)
	}

	// --- ⟨1⟩: the arrival takes the ArrivalRefRetracted disposition ----
	// SEAM W9B-S-DISP: structural, not a literal — see w9b_seams_test.go.
	disp := w9bDisposition(t, st, a1.ArrivalID)
	if disp == "" || w9bIsDeliveredDisposition(disp) {
		t.Fatalf("W9b-1 ⟨1⟩ (m22's state half): the retracted arrival %s carries disposition %q, which is one of the five DELIVERED dispositions %v (or empty). "+
			"F4 §3.6 gives the skipped arrival a NEW disposition, `ArrivalRefRetracted` — a review-lane row binding nothing. "+
			"An arrival left as `%s` is one the fold still treats as this trip's",
			a1.ArrivalID, disp, w9bDeliveredDispositions, ArrivalExpected)
	}

	// --- ⟨1⟩: the `visit` table is byte-identical throughout ----------
	// F4 §3.6: "this event never touches the visit table (I-1)". The crew
	// really is standing at the destination; the retraction repudiates the
	// SELECTION, not the sighting.
	if got := w9bSection(t, st, "visit"); got != visitsBefore {
		t.Fatalf("W9b-1 ⟨1⟩ (the helpful fold): the `visit` projection CHANGED across the retraction.\nbefore: %s\nafter:  %s\n"+
			"F4 §3.6: the un-reference never touches the visit table (I-1) — the people ARE here, and deleting their visits would take a crew off the muster on the strength of a clerical correction",
			visitsBefore, got)
	}
}

// TestW9b1RetractionIsOrderIndependentAndDanglingTolerant is ⟨1⟩'s
// permutation half: both delivery permutations — including the DANGLING case,
// where the retraction is delivered before the arrival it names — yield
// byte-identical `DumpProjections`.
//
// KILLER MUTANT: m14b — apply-time validation dropping a dangling retraction.
// F4 §3.6: "The fold is a bare INSERT — no lookup, no validation at apply
// time (dangling-tolerant: a retraction delivered before its arrival folds
// inert)".
//
// ⚠ THE NON-DEGENERACY GUARD, and it is the whole reason this test is not
// decoration: two stores that BOTH did nothing are byte-identical too. m14 —
// the no-op un-bind — is byte-identically green in both permutations. So the
// dumps are compared AND both stores are asserted to have reopened the trip.
// A permutation test without that second assertion certifies the mutant.
//
// The two streams are deliberately different: the arrival is authored at the
// DESTINATION and the retraction at the ORIGIN. Both on one stream would make
// the dangling permutation a within-stream seq GAP, which the store refuses
// outright (§8.4, never a silent skip) — so the cross-post fixture is not a
// convenience, it is the only delivery the dangling case can have.
func TestW9b1RetractionIsOrderIndependentAndDanglingTolerant(t *testing.T) {
	// Build the event set ONCE, so both permutations replay identical bytes.
	src := w9bNewStore(t)
	w9bSeedPersons(t, src, "site-a", 1, "drv-1", "occ-1")
	x := w9bDispatchOpen(t, src, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-1", []string{"occ-1"}, 240)
	a1 := w9bArriveExpected(t, src, "site-b", 1, x.TripID)
	retraction := w9bApplyNext(t, src, "site-a", 1, w9bBase+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))

	siteA := w9bEventsOf(t, src, "site-a", 1) // persons, dispatch, retraction
	siteB := w9bEventsOf(t, src, "site-b", 1) // the arrival
	if len(siteB) != 1 {
		t.Fatalf("w9b FIXTURE DEFECT: site-b authored %d events, want exactly the arrival", len(siteB))
	}
	if siteA[len(siteA)-1].Key() != retraction.Key() {
		t.Fatalf("w9b FIXTURE DEFECT: site-a's last event is %v, want the retraction %v", siteA[len(siteA)-1].Key(), retraction.Key())
	}

	// Permutation 1 — the DANGLING case: the whole origin stream first, so
	// the retraction lands before the arrival it names exists here at all.
	dangling := w9bNewStore(t)
	for _, e := range siteA {
		w9bApply(t, dangling, e)
	}
	w9bApply(t, dangling, siteB[0])

	// Permutation 2 — the ordinary case: the arrival first, retraction last.
	ordinary := w9bNewStore(t)
	for _, e := range siteA[:len(siteA)-1] {
		w9bApply(t, ordinary, e)
	}
	w9bApply(t, ordinary, siteB[0])
	w9bApply(t, ordinary, siteA[len(siteA)-1])

	// --- THE NON-DEGENERACY GUARD, asserted BEFORE the byte comparison ---
	for _, arm := range []struct {
		name string
		st   *store.Store
	}{
		{"dangling (retraction delivered first)", dangling},
		{"ordinary (arrival delivered first)", ordinary},
	} {
		if got := w9bTripState(t, arm.st, x.TripID); got != TripStateOpen {
			t.Fatalf("W9b-1 ⟨1⟩ permutation %s (m14 — the no-op un-bind): trip %s reads %q, want %q. "+
				"Byte-identity between two stores that BOTH ignored the retraction is vacuous, so this assertion runs first",
				arm.name, x.TripID, got, TripStateOpen)
		}
		if !w9bHasExpectation(t, arm.st, "site-b", x.TripID) {
			t.Fatalf("W9b-1 ⟨1⟩ permutation %s: trip %s has no open expectation at site-b after the retraction", arm.name, x.TripID)
		}
	}

	// --- byte-identical DumpProjections across the permutations --------
	d1 := w9bAllProjections(t, dangling)
	d2 := w9bAllProjections(t, ordinary)
	if d1 != d2 {
		t.Fatalf("W9b-1 ⟨1⟩ (m14b — apply-time validation): the two delivery permutations of the SAME event set diverge.\n"+
			"dangling-first: %s\narrival-first:  %s\n"+
			"F4 §3.6: the fold is a bare INSERT, dangling-tolerant — a retraction delivered before its arrival folds inert and the recompute reaches the same state. "+
			"A fold that drops or refuses the dangling retraction is silently correct at one post and wrong at the other",
			d1, d2)
	}
}

// =========================================================================
// W9b-1 ⟨3⟩ — READS `cancelled` (T12, second half)
// =========================================================================

// TestW9b1CancelledTripReadsCancelledAfterRetraction is ⟨3⟩: a trip that was
// ALSO cancelled reads `cancelled` after the retraction — never a hardcoded
// open.
//
// KILLER MUTANT: the state-machine shortcut that hardcodes reopen-to-open
// (rev 3's unnumbered TE-8(4), subsumed by m21 at T12b). Here it is caught in
// its own right: a build whose retraction branch writes `open` unconditionally
// is green on ⟨1⟩ and wrong on a trip whose origin called it off.
func TestW9b1CancelledTripReadsCancelledAfterRetraction(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-c", "occ-c")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "CANCEL TRUCK", "drv-c", []string{"occ-c"}, 240)

	// The origin calls it off FIRST — `CancelTrip` refuses an arrived trip,
	// so this is the only order in which the arrived-after-cancel world is
	// constructible through the authoring path.
	if err := CancelTrip(st, "site-a", 1, x.TripID, w9bStr("Ice Road"), 0, "default"); err != nil {
		t.Fatalf("w9b fixture: cancel %s: %v", x.TripID, err)
	}
	a1 := w9bArriveExpected(t, st, "site-b", 1, x.TripID)
	if got := w9bTripState(t, st, x.TripID); got != TripStateArrivedAfterCancel {
		t.Fatalf("PRECONDITION: trip %s is %q, want %q — ⟨3⟩'s fixture is a trip carrying BOTH facts", x.TripID, got, TripStateArrivedAfterCancel)
	}

	w9bApplyNext(t, st, "site-a", 1, w9bBase+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))

	got := w9bTripState(t, st, x.TripID)
	if got == TripStateOpen {
		t.Fatalf("W9b-1 ⟨3⟩ (the state-machine shortcut): after the retraction trip %s reads %q — a HARDCODED open. "+
			"The cancel is still a surviving raw fact; F4 §3.6 says the trip reverts to whatever the surviving raw facts say, and here that is %q",
			x.TripID, got, TripStateCancelled)
	}
	if got != TripStateCancelled {
		t.Fatalf("W9b-1 ⟨3⟩: after the retraction trip %s reads %q, want %q", x.TripID, got, TripStateCancelled)
	}
	if w9bHasExpectation(t, st, "site-b", x.TripID) {
		t.Fatalf("W9b-1 ⟨3⟩: trip %s reads `cancelled` but site-b holds an open expectation for it — a cancelled trip is not an expectation and must not alert", x.TripID)
	}
}

// =========================================================================
// W9b-1 ⟨2⟩(a) — STAYS `arrived`: a surviving explicit reference (T12b)
// =========================================================================

// TestW9b1DoublyReferencedTripStaysArrivedAndTheBindingMoves is ⟨2⟩(a), all
// five assertions plus the tail.
//
// KILLER MUTANT: m21 — the unconditional reopen. A build that recomputes the
// trip's state from the cancel set and the retraction alone, without
// re-running Pass 1's reference scan, is green on T12 both halves, green on
// T13, green on T13b, and it reopens a trip a second arrival still
// legitimately binds — restoring an expectation, a board row and an alert at
// both posts for a vehicle whose crew a second post checked in.
//
// ⚠ m19 IS NOT KILLED HERE. The reference-only carve-out finds the surviving
// reference and behaves correctly in this world. m19's fixture is T12c.
//
// ⚠ THE TAIL IS THE POSITIVE CONTROL. Without it, a build that never reopens
// anything satisfies every assertion above it. Retracting A2 as well must
// reopen X.
func TestW9b1DoublyReferencedTripStaysArrivedAndTheBindingMoves(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-2", "occ-2")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "DOUBLE TRUCK", "drv-2", []string{"occ-2"}, 240)

	// A1 and A2 both carry TripRef = X. Same site, same epoch, ascending
	// seq, so A1 is canonically first and Pass 1 gives it `boundTo`
	// [F4 §12 T12b: "a world constructible in the delivered tree with no new
	// code, since Pass 1 records the second as ArrivalExpected"].
	const a1ID, a2ID = "arr-A1", "arr-A2"
	w9bApplyNext(t, st, "site-b", 1, w9bBase+w9bMinute, w9bWireArrival(a1ID, x.TripID,
		[]ArrivalVisit{{VisitID: a1ID + "-v0", PersonID: "drv-2"}}, w9bStr("Pad Site Control")))
	w9bApplyNext(t, st, "site-b", 1, w9bBase+2*w9bMinute, w9bWireArrival(a2ID, x.TripID,
		[]ArrivalVisit{{VisitID: a2ID + "-v0", PersonID: "occ-2"}}, w9bStr("Pad Site Control")))

	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("PRECONDITION: trip %s is %q, want %q", x.TripID, got, TripStateArrived)
	}
	if got := w9bBoundArrival(t, st, x.TripID); got != a1ID {
		t.Fatalf("PRECONDITION: trip %s is bound to %q, want the canonically FIRST arrival %q — the T12b world needs the winner retracted, not the runner-up",
			x.TripID, got, a1ID)
	}

	w9bApplyNext(t, st, "site-a", 1, w9bBase+3*w9bMinute, w9bRetraction(a1ID, w9bStr("Ice Road")))

	// --- 1. X stays `arrived` -----------------------------------------
	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("W9b-1 ⟨2⟩(a) (m21 — the unconditional reopen): after retracting %s, trip %s reads %q, want %q.\n"+
			"[[D-58]]: the trip stays closed where something else still BINDS it, and %s is a surviving explicit reference. "+
			"Reopening here restores an expectation, a board row and an alert at BOTH posts for a vehicle whose crew a second post checked in",
			a1ID, x.TripID, got, TripStateArrived, a2ID)
	}

	// --- 2. `trip_state.arrival_id` moves A1 -> A2, as a LITERAL --------
	if got := w9bBoundArrival(t, st, x.TripID); got != a2ID {
		t.Fatalf("W9b-1 ⟨2⟩(a): trip %s's `trip_state.arrival_id` is %q, want the literal %q. "+
			"Pass 1 promotes the runner-up (F4 §3.6, `trip.go`'s \"First arrival in canonical order wins a doubly-referenced trip\"); "+
			"the expected value is pinned as the arrival id itself, never as `expected(recompute)`",
			x.TripID, got, a2ID)
	}

	// --- 3. the expectation stays CLOSED, at both posts ----------------
	// (The "no alert at either post" half is the same fact observed at the
	// deadline board; it is asserted in internal/web, which can import it.)
	for _, site := range []string{"site-a", "site-b"} {
		if w9bHasExpectation(t, st, site, x.TripID) {
			t.Fatalf("W9b-1 ⟨2⟩(a): %s holds an OPEN expectation for trip %s after the retraction — the trip is still `arrived` and must not be expected, or alerting, anywhere", site, x.TripID)
		}
	}

	// --- 4/5. dispositions ---------------------------------------------
	d1 := w9bDisposition(t, st, a1ID)
	if d1 == "" || w9bIsDeliveredDisposition(d1) {
		t.Fatalf("W9b-1 ⟨2⟩(a): retracted arrival %s carries disposition %q — want the NEW `ArrivalRefRetracted` disposition, not one of the five delivered %v (SEAM W9B-S-DISP)",
			a1ID, d1, w9bDeliveredDispositions)
	}
	if d2 := w9bDisposition(t, st, a2ID); d2 != ArrivalExpected {
		t.Fatalf("W9b-1 ⟨2⟩(a): surviving arrival %s carries disposition %q, want %q — the retraction names ONE arrival and touches no other",
			a2ID, d2, ArrivalExpected)
	}

	// --- THE TAIL: the positive control --------------------------------
	w9bApplyNext(t, st, "site-a", 1, w9bBase+4*w9bMinute, w9bRetraction(a2ID, w9bStr("Ice Road")))

	if got := w9bTripState(t, st, x.TripID); got != TripStateOpen {
		t.Fatalf("W9b-1 ⟨2⟩(a) TAIL — THE POSITIVE CONTROL FAILED: with BOTH references retracted, trip %s reads %q, want %q.\n"+
			"Without this arm, every assertion above is satisfied by a build that never reopens anything — the over-correction of m21, and just as wrong",
			x.TripID, got, TripStateOpen)
	}
	if !w9bHasExpectation(t, st, "site-b", x.TripID) {
		t.Fatalf("W9b-1 ⟨2⟩(a) TAIL: trip %s reopened but site-b holds no open expectation for it — the expectation and the alert must come BACK", x.TripID)
	}
}

// =========================================================================
// W9b-1 ⟨2⟩(b) — STAYS `arrived`: a Pass-2 auto-correlation (T12c)
// =========================================================================

// TestW9b1CorrelationRebindsAndTheTripStaysArrived is ⟨2⟩(b) — [[D-58]]'s
// world, which no rev-3 fixture reached.
//
// KILLER MUTANT: m19 — the reference-only carve-out. A build that implements
// "stays arrived if another arrival still REFERENCES it" queries
// `trip_arrival.trip_ref`, finds nothing naming X, reopens X and renders
// "OPEN — its alert is live", while `boundTo` says `arrived` and no alert
// exists. It is GREEN on T12b, which is exactly why this fixture exists.
//
// The load-bearing precondition, stated as F4 §12 T12c states it: U is
// `ArrivalPlain` before the retraction, because Pass 2 excludes X (already
// bound by A1) and nothing else matches. If U were already correlated the
// fixture would prove nothing — Pass 1 and Pass 2 are ONE call, so freeing X
// in Pass 1 must ENLARGE Pass 2's candidate set inside the same recompute.
func TestW9b1CorrelationRebindsAndTheTripStaysArrived(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-3", "occ-3", "pax-u")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "F-350", "drv-3", []string{"occ-3"}, 240)

	a1 := w9bArriveExpected(t, st, "site-b", 1, x.TripID)
	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("PRECONDITION: trip %s is %q, want %q after the mis-click", x.TripID, got, TripStateArrived)
	}

	// U: an unsolicited arrival at the destination carrying X's correlation
	// key — same vehicle, same claimed origin — and NO expected-row click.
	// ⭐ ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ RE-ARMED — and this site is why the re-seed had
	// to reach the arrival half at all. RESEED-164.md §3.9 named this test's `ArrivalPlain` arm as
	// the SECOND vacuous green it had no site in. The load-bearing precondition below is that U is
	// PLAIN *because Pass 2 excludes X (already bound by A1)* — but with a number in the id slot U
	// was plain because it could match NOTHING, and then the movement plain -> correlated after the
	// retraction, which IS this criterion's observable, could not occur at all.
	// U now carries a vehicle RECORD id, so the precondition means what it says and the movement is
	// real. PRODUCED: the whole of `w9b_unbind_test.go` is green in the curated isolated copy.
	// See reports/window-build/ARRIVAL-RESEED.md.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "u, err := RecordUnsolicitedArrival(st, "site-b", 1, "F-350", "site-a", []string{"pax-u"}, w9bStr("Pad Site Control"), 0, "default")"⟧
	u, err := RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", "F-350"), "site-a", []string{"pax-u"}, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: unsolicited arrival U: %v", err)
	}
	if got := w9bDisposition(t, st, u.ArrivalID); got != ArrivalPlain {
		t.Fatalf("PRECONDITION (F4 §12 T12c, load-bearing): U (%s) is %q, want %q. "+
			"Pass 2 must EXCLUDE X because A1 already bound it; a U that is already correlated makes this fixture prove nothing about the retraction enlarging the candidate set",
			u.ArrivalID, got, ArrivalPlain)
	}

	w9bApplyNext(t, st, "site-a", 1, w9bBase+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))

	// --- X stays `arrived` — bound by a correlation, not a reference ---
	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("W9b-1 ⟨2⟩(b) (m19 — the reference-only carve-out): after retracting %s, trip %s reads %q, want %q.\n"+
			"No arrival REFERENCES X any more — and [[D-58]] rules the condition is `boundTo`, not `trip_ref`. Pass 2's auto-correlation carries NO reference and NO human selection, "+
			"and it binds X inside the same recompute that freed it. A build that queries `trip_arrival.trip_ref` finds nothing, reopens X, and tells the officer an alert returned that did not",
			a1.ArrivalID, x.TripID, got, TripStateArrived)
	}

	// --- `trip_state.arrival_id` = U, pinned as a literal ---------------
	if got := w9bBoundArrival(t, st, x.TripID); got != u.ArrivalID {
		t.Fatalf("W9b-1 ⟨2⟩(b): trip %s's `trip_state.arrival_id` is %q, want the literal %q (U)", x.TripID, got, u.ArrivalID)
	}

	// --- U's disposition moves ArrivalPlain -> ArrivalCorrelated --------
	if got := w9bDisposition(t, st, u.ArrivalID); got != ArrivalCorrelated {
		t.Fatalf("W9b-1 ⟨2⟩(b): U (%s) carries disposition %q, want %q — the movement `%s` → `%s` IS the observable that the retraction enlarged Pass 2's candidate set",
			u.ArrivalID, got, ArrivalCorrelated, ArrivalPlain, ArrivalCorrelated)
	}

	// --- no expectation (hence no alert) at either post -----------------
	for _, site := range []string{"site-a", "site-b"} {
		if w9bHasExpectation(t, st, site, x.TripID) {
			t.Fatalf("W9b-1 ⟨2⟩(b): %s holds an OPEN expectation for trip %s — X is still `arrived` and must not alert anywhere", site, x.TripID)
		}
	}

	// --- the retracted arrival still lands on the review lane ----------
	if d := w9bDisposition(t, st, a1.ArrivalID); d == "" || w9bIsDeliveredDisposition(d) {
		t.Fatalf("W9b-1 ⟨2⟩(b): retracted arrival %s carries disposition %q — want the NEW `ArrivalRefRetracted` disposition (SEAM W9B-S-DISP)", a1.ArrivalID, d)
	}
}

// TestW9b1CorrelationRebindsWhenTheCorrelatingArrivalCROSSES is ⟨2⟩(b)'s
// second half — the CROSSING case, at the fold level: U is authored at the
// peer and DELIVERED AFTER the retraction is appended at the origin.
//
// State is a pure function of the raw tables, so the final state must be the
// same one T12c's first half reaches. The half of the crossing case that
// cannot be tested here — that the CONFIRMATION, being a read at render time,
// is still correct — is asserted in internal/web, where the 303's target can
// be re-fetched after U lands.
//
// KILLER MUTANT: m20 — the pre-append prediction. At the fold level a
// prediction leaves no trace, so the fold-side arm of this crossing case is a
// determinism assertion; the mutant itself dies on the rendered page.
func TestW9b1CorrelationRebindsWhenTheCorrelatingArrivalCROSSES(t *testing.T) {
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "drv-4", "occ-4", "pax-u2")
	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "F-350", "drv-4", []string{"occ-4"}, 240)
	a1 := w9bArriveExpected(t, st, "site-b", 1, x.TripID)

	// U is authored at the peer but HELD BACK — it does not exist here when
	// the retraction is appended.
	uID := "arr-U-crossing"
	uEnv := w9bBuildNext(t, st, "site-b", 1, w9bBase+w9bMinute, TripArrival{
		Type: "trip_arrival", ArrivalID: uID,
		VehicleEntered: w9bStr("F-350"), OriginSite: w9bStr("site-a"),
		Visits:     []ArrivalVisit{{VisitID: uID + "-v0", PersonID: "pax-u2"}},
		RecordedBy: w9bStr("Pad Site Control"),
	})

	w9bApplyNext(t, st, "site-a", 1, w9bBase+2*w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))
	if got := w9bTripState(t, st, x.TripID); got != TripStateOpen {
		t.Fatalf("W9b-1 crossing case, BEFORE U lands: trip %s reads %q, want %q — with nothing binding it yet, the retraction must reopen it",
			x.TripID, got, TripStateOpen)
	}

	// Now the peer's arrival crosses the wire.
	w9bApply(t, st, uEnv)

	if got := w9bTripState(t, st, x.TripID); got != TripStateArrived {
		t.Fatalf("W9b-1 ⟨2⟩(b) crossing case: after U (%s) is delivered, trip %s reads %q, want %q — Pass 2 binds it at exactly one candidate",
			uID, x.TripID, got, TripStateArrived)
	}
	if got := w9bBoundArrival(t, st, x.TripID); got != uID {
		t.Fatalf("W9b-1 ⟨2⟩(b) crossing case: trip %s is bound to %q, want the literal %q", x.TripID, got, uID)
	}
	if got := w9bDisposition(t, st, uID); got != ArrivalCorrelated {
		t.Fatalf("W9b-1 ⟨2⟩(b) crossing case: U (%s) is %q, want %q", uID, got, ArrivalCorrelated)
	}
}
