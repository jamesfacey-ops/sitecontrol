// w9c_rearm_test.go — W9c-1, W9c-2, W9c-3: the re-arm, the re-suppression,
// and the set-membership-never-arithmetic pin.
//
// design/WINDOW-BUILD-PLAN-v1.md "#### W9c", criteria RE-LOCKED 2026-09-02
// against design/alert-unack-v1.md revision 4 (§7 rows T1, T2, T2b).
//
// ⚠ COMPILE-CLEAN BY DESIGN — see w9c_seams_test.go's header. Every red
// below is a RUNTIME red on the pre-chunk tree, not a build failure.
//
// KILLER MUTANTS: m1 (the no-op fold) on W9c-1; m2 (one-way trip-scoped
// resolution) on W9c-2; m2b (arithmetic effectiveness) on W9c-3.
package deadline

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// w9cAlertingTrip builds the fixture every criterion here rests on: one
// inbound expectation whose deadline passed while the link was down, so it
// sits in the review lane and REACHES THE BANNER while unacked. The
// precondition is asserted, not assumed — "Banner() empty" proves nothing
// on a board that was empty anyway.
func w9cAlertingTrip(t *testing.T, st *store.Store, d *Deadlines) (tripID string, dispatch store.Envelope) {
	t.Helper()
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary (see fix/deadline-flake)
	b := mustEval(t, d)
	if !w9cBannerHas(b, trip.TripID) {
		t.Fatalf("w9c fixture: the trip does not reach the banner before any ack (%s) — every 'suppressed' assertion below would be vacuous", w9cBannerStatuses(b))
	}
	return trip.TripID, env
}

// TestW9c1UnAckReArmsTheBannerAndDeletesNothing — W9c-1 (T1).
//
// "Un-ack re-arms: the retraction folds → the red row is back in Banner(),
// Acked == false, both events still in the log — nothing deleted."
//
// MUST FAIL ON: m1, the no-op fold — the retraction event ships and folds
// but ackedTrips still reads bare ∃-ack. That is precisely the pre-chunk
// tree, which is why this criterion's pre-chunk red is the real thing and
// not a plant.
func TestW9c1UnAckReArmsTheBannerAndDeletesNothing(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	tripID, _ := w9cAlertingTrip(t, st, d)

	ack := w9cAckHere(t, st, tripID, nil)
	if b := mustEval(t, d); len(b.Banner()) != 0 {
		t.Fatalf("W9c-1 fixture: after the ack the banner is not empty: %s", w9cBannerStatuses(b))
	}

	// The retraction is authored at the OTHER post — an ack silences both
	// posts (F6 §1 sharpening 2), so the correction must replicate too.
	retraction := w9cRetract(t, st, w9cKey{origin, 1, 0}, ack, "Pad Site Control", 1_700_000_500_000)

	b := mustEval(t, d)
	rows := w9cBannerRows(b, tripID)
	if len(rows) != 1 {
		t.Fatalf("W9c-1: after the retraction of ack %v the trip has %d banner rows, want exactly 1 — the un-ack did not re-arm the alert (banner: %s)",
			ack, len(rows), w9cBannerStatuses(b))
	}
	if rows[0].Acked {
		t.Fatalf("W9c-1: the re-armed row still reports Acked == true — the predicate is reading bare ack existence, not ∃ unretracted ack")
	}
	if a := findAlert(t, b, tripID); a.Acked {
		t.Fatalf("W9c-1: the board row for %s still reports Acked == true after its only ack was retracted", tripID)
	}

	// Nothing is deleted (D-1): both events are still in the log.
	if !w9cHasEvent(t, st, ack) {
		t.Fatalf("W9c-1: the ack event %v is no longer in the log — a retraction must never delete its target", ack)
	}
	if !w9cHasEvent(t, st, retraction) {
		t.Fatalf("W9c-1: the retraction event %v is not in the log", retraction)
	}
	if got := w9cRetractionTargets(t, st); len(got) != 1 || got[0] != ack {
		t.Fatalf("W9c-1: retractions in the log name %v, want exactly [%v]", got, ack)
	}
}

// TestW9c2ReSuppressionAfterAnUnAckWorksAndIsItselfRetractable — W9c-2 (T2).
//
// "Re-suppression works: ack → retract → re-ack → suppressed; retract
// again → alerting."
//
// MUST FAIL ON: m2 — a retraction implemented trip-scoped ("any retraction
// kills all acks for this trip forever"), which makes re-suppression
// impossible: the re-ack lands and the banner never goes quiet.
func TestW9c2ReSuppressionAfterAnUnAckWorksAndIsItselfRetractable(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	tripID, _ := w9cAlertingTrip(t, st, d)

	ack1 := w9cAckHere(t, st, tripID, nil)
	if b := mustEval(t, d); len(b.Banner()) != 0 {
		t.Fatalf("W9c-2 fixture: ack1 did not suppress: %s", w9cBannerStatuses(b))
	}

	r1 := w9cRetract(t, st, w9cKey{origin, 1, 0}, ack1, "Pad Site Control", 1_700_000_500_000)
	if b := mustEval(t, d); !w9cBannerHas(b, tripID) {
		t.Fatalf("W9c-2: retracting ack1 left the trip suppressed — there is nothing for the officer to RE-acknowledge (banner: %s)", w9cBannerStatuses(b))
	}

	// The re-ack. This is an ordinary new alert_acknowledged, never a
	// retraction of a retraction (F6 §2).
	ack2 := w9cAckHere(t, st, tripID, nil)
	if b := mustEval(t, d); len(b.Banner()) != 0 {
		t.Fatalf("W9c-2: after the RE-ack (%v) the trip is still alerting — re-suppression is impossible, so the officer can never dismiss this alert again (banner: %s)",
			ack2, w9cBannerStatuses(b))
	}

	r2 := w9cRetract(t, st, w9cKey{origin, 1, 0}, ack2, "Pad Site Control", 1_700_000_600_000)
	if b := mustEval(t, d); !w9cBannerHas(b, tripID) {
		t.Fatalf("W9c-2: retracting ack2 (%v) did not re-arm the alert (banner: %s)", r2, w9cBannerStatuses(b))
	}

	for _, k := range []w9cKey{ack1, r1, ack2, r2} {
		if !w9cHasEvent(t, st, k) {
			t.Fatalf("W9c-2: event %v is no longer in the log — nothing is ever deleted (D-1)", k)
		}
	}
}

// TestW9c3TwoRetractionsOfOneAckThenAFreshReAckStillSuppresses — W9c-3
// (T2b).
//
// "Set membership, never arithmetic: TWO retractions naming ONE ack, then a
// fresh re-ack → suppressed, under both delivery permutations — the fixture
// permutation testing alone is blind to."
//
// MUST FAIL ON: m2b — `count(acks) − count(retractions)`, which yields
// 2 − 2 = 0 and silently no-ops the officer's re-ack forever. m2b is
// ORDER-INDEPENDENT by construction, so no amount of permutation reddens
// it; this fixture's shape is the only thing that does.
//
// ⚠ NO PRE-CHUNK RED. On the delivered tree the two retractions fold into
// nothing, ack1 and ack2 both stand, and the trip is suppressed — the
// asserted answer, reached for the wrong reason. Its evidence is therefore
// the PLANTED m2b control, never this clean pass. See
// reports/window-build/W9c-tests.md §W9c-3.
//
// The permutation set is every interleaving of the two site streams that
// preserves within-stream order — the delivery orders replication can
// actually produce.
func TestW9c3TwoRetractionsOfOneAckThenAFreshReAckStillSuppresses(t *testing.T) {
	// One template store supplies the dispatch envelope every permutation
	// store replays; the trip's own key is then identical everywhere.
	tmpl := newSite(t)
	tmplD := newScanner(t, tmpl)
	tripID, dispatch := w9cAlertingTrip(t, tmpl, tmplD)

	// STATED fixture keys — never inherited. ack1 is site-b's; the two
	// retractions are one per site (F6 §3.4's crossing: both posts un-ack
	// before either retraction replicates); ack2 — the fresh re-ack — is
	// authored at "site-a", the LOWER-NAMED site, stated here as a fixture
	// parameter so this fixture also reddens the key-constrained pairing
	// variant (F6 §7, T2b's footnote).
	const ack2Site = origin // "site-a" < "site-b"
	ack1 := w9cKey{here, 1, 1}
	retrB := w9cKey{here, 1, 2}
	retrA := w9cKey{origin, 1, 2}
	ack2 := w9cKey{ack2Site, 1, 3}
	if ack2Site >= here {
		t.Fatalf("W9c-3 fixture: the re-ack site %q must be the LOWER-named of the two posts (%q, %q)", ack2Site, origin, here)
	}

	streamA := []store.Envelope{
		dispatch, // (site-a, 1, 1)
		w9cRetractionEnvelope(t, tmpl, retrA, ack1, "Pad Site Control", 1_700_000_400_000),
		w9cAckEnvelope(ack2, tripID, nil, nil, "Pad Site Control", 1_700_000_900_000),
	}
	streamB := []store.Envelope{
		w9cAckEnvelope(ack1, tripID, nil, nil, "Ice Camp B", 1_700_000_100_000),
		w9cRetractionEnvelope(t, tmpl, retrB, ack1, "Ice Camp B", 1_700_000_500_000),
	}
	for i, e := range streamA {
		if got := (w9cKey{e.SiteID, e.NodeEpoch, e.Seq}); got.Seq != int64(i+1) || got.Site != origin {
			t.Fatalf("W9c-3 fixture: stream A event %d is at %v, want (%s, epoch 1, seq %d)", i, got, origin, i+1)
		}
	}

	// CONTROL for the fixture itself: with no ack in the stream at all the
	// trip ALERTS. Without this, "Banner() empty" would be satisfiable by a
	// board that never had a row.
	ctrl := newSite(t)
	ctrlD := newScanner(t, ctrl)
	w9cApply(t, ctrl, dispatch)
	ctrlD.OnApplied(dispatch, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary
	if b := mustEval(t, ctrlD); !w9cBannerHas(b, tripID) {
		t.Fatalf("W9c-3 fixture control: the unacked trip does not alert (%s) — every suppression assertion below would be vacuous", w9cBannerStatuses(b))
	}

	for _, order := range w9cInterleavings(streamA, streamB) {
		t.Run(w9cOrderName(order), func(t *testing.T) {
			st := newSite(t)
			d := newScanner(t, st)
			for _, e := range order {
				w9cApply(t, st, e)
				if e.SiteID == dispatch.SiteID && e.Seq == dispatch.Seq {
					d.OnApplied(e, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary
				}
			}
			b := mustEval(t, d)
			if w9cBannerHas(b, tripID) {
				t.Fatalf("W9c-3: two retractions both naming ack %v, then a fresh re-ack at %v, and the trip is ALERTING — effectiveness is being computed as count(acks) − count(retractions) (2 − 2 = 0), so the officer's re-acknowledgment is a permanent silent no-op. Resolution is set membership over NAMED targets, never cardinality (F6 §3.3). Banner: %s",
					ack1, ack2, w9cBannerStatuses(b))
			}
			// The retractions really did name ONE ack — a fixture in which
			// they named two different acks would prove nothing about
			// cardinality.
			targets := w9cRetractionTargets(t, st)
			if len(targets) != 2 || targets[0] != ack1 || targets[1] != ack1 {
				t.Fatalf("W9c-3 fixture: the log's retractions name %v, want both of them naming %v", targets, ack1)
			}
		})
	}
}

// w9cInterleavings returns every merge of a and b that preserves the order
// within each — the set of delivery orders two replicated streams can
// actually produce.
func w9cInterleavings(a, b []store.Envelope) [][]store.Envelope {
	if len(a) == 0 {
		return [][]store.Envelope{append([]store.Envelope{}, b...)}
	}
	if len(b) == 0 {
		return [][]store.Envelope{append([]store.Envelope{}, a...)}
	}
	var out [][]store.Envelope
	for _, rest := range w9cInterleavings(a[1:], b) {
		out = append(out, append([]store.Envelope{a[0]}, rest...))
	}
	for _, rest := range w9cInterleavings(a, b[1:]) {
		out = append(out, append([]store.Envelope{b[0]}, rest...))
	}
	return out
}

// w9cOrderName names one delivery order by its site letters, so a failing
// subtest says which permutation broke.
func w9cOrderName(order []store.Envelope) string {
	name := ""
	for _, e := range order {
		if e.SiteID == origin {
			name += "A"
		} else {
			name += "B"
		}
	}
	return name
}
