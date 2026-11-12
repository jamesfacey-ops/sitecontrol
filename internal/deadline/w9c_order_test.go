// w9c_order_test.go — W9c-4: no order of any kind.
//
// design/WINDOW-BUILD-PLAN-v1.md "#### W9c", criteria RE-LOCKED 2026-09-02
// against design/alert-unack-v1.md revision 4 (§7 row T3).
//
// KILLER MUTANTS: m3 (order-dependent apply — the fold validates or drops a
// DANGLING retraction), m3b (arrival-order resolution by rowid/clock), and
// above all m3c (THE KEY-PRECEDENCE RESOLVER — "a retraction cancels its
// target only if the retraction's key exceeds the target's", F6 §5's named
// likeliest accidental implementation, which dies ONLY on the reverse
// pair).
package deadline

import (
	"fmt"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// w9cPairFixture is one ack + one retraction of it, on two named streams,
// plus the trip they concern. `ackSite` and `retrSite` are STATED by the
// caller — never inherited — because the whole discriminator of this
// criterion is which of the two keys is greater.
type w9cPairFixture struct {
	name     string
	ackSite  string
	retrSite string
}

// TestW9c4NoOrderOfAnyKindResolvesTheRetraction — W9c-4 (T3).
//
// "No order of any kind: forward AND reverse key-direction pairs (a
// retraction whose key is BELOW its target ack's must still take full
// effect), each delivered in both orders plus a ≥3-event crossing set →
// byte-identical DumpProjections and identical Banner() in every
// permutation; fixture site ids stated, never inherited."
//
// The site ids are stated here and the ordering they induce is asserted,
// so a later rename that made "site-b" the lower-named post could not
// silently turn the reverse pair into a second forward pair:
//
//	key order is (site_id, node_epoch, seq) — cross-site it is decided at
//	site_id, and the fixture pins "site-a" < "site-b".
//
// Each retraction is given an EARLIER authored wall instant than the ack it
// competes with, so a fold that consulted the clock would order these
// events the opposite way from the key — and, since the correct answer
// consults NEITHER, both orderings must produce the same board.
func TestW9c4NoOrderOfAnyKindResolvesTheRetraction(t *testing.T) {
	if !(origin < here) {
		t.Fatalf("W9c-4 fixture: this test's forward/reverse distinction rests on %q < %q — the stated site ids no longer order that way", origin, here)
	}

	for _, fx := range []w9cPairFixture{
		// Forward: ack at site-a, retraction at site-b → the retraction's
		// key is ABOVE its target's. A key-precedence resolver (m3c)
		// survives this one, which is exactly why it is not enough.
		{name: "forward_retraction_key_above_target", ackSite: origin, retrSite: here},
		// Reverse: ack at site-b, retraction at site-a → the retraction's
		// key is BELOW its target's. m3c dies here and nowhere else.
		{name: "reverse_retraction_key_below_target", ackSite: here, retrSite: origin},
	} {
		t.Run(fx.name, func(t *testing.T) {
			tmpl := newSite(t)
			tmplD := newScanner(t, tmpl)
			tripID, dispatch := w9cAlertingTrip(t, tmpl, tmplD)

			// Stated keys. The dispatch owns (site-a, 1, 1), so a site-a
			// ack or retraction takes seq 2 and the site-b one takes seq 1.
			ackKey := w9cKey{fx.ackSite, 1, 1}
			retrKey := w9cKey{fx.retrSite, 1, 1}
			if fx.ackSite == origin {
				ackKey.Seq = 2
			} else {
				retrKey.Seq = 2
			}
			if fx.name == "forward_retraction_key_above_target" && !w9cKeyLess(ackKey, retrKey) {
				t.Fatalf("W9c-4 fixture: the FORWARD pair must have the retraction key %v ABOVE the ack key %v", retrKey, ackKey)
			}
			if fx.name == "reverse_retraction_key_below_target" && !w9cKeyLess(retrKey, ackKey) {
				t.Fatalf("W9c-4 fixture: the REVERSE pair must have the retraction key %v BELOW the ack key %v", retrKey, ackKey)
			}

			// The retraction is authored EARLIER on the wall clock than its
			// own target ack: any clock consultation flips this pair.
			ackEnv := w9cAckEnvelope(ackKey, tripID, nil, nil, "post "+ackKey.Site, 1_700_000_800_000)
			retrEnv := w9cRetractionEnvelope(t, tmpl, retrKey, ackKey, "post "+retrKey.Site, 1_700_000_100_000)

			streams := w9cSplitStreams(dispatch, ackEnv, retrEnv)
			w9cAssertPermutationsAgree(t, tripID, dispatch, streams[origin], streams[here],
				true, // the retraction is fully effective ⇒ the trip ALERTS
				fmt.Sprintf("W9c-4 (%s): the retraction at %v does not take full effect against its target ack at %v", fx.name, retrKey, ackKey))
		})
	}

	// The ≥3-event crossing set: ack1, retraction-of-ack1, ack2 across the
	// two streams, every arrival order. ack2 stands unretracted, so the
	// trip is SUPPRESSED in every one of them.
	t.Run("three_event_crossing_set", func(t *testing.T) {
		tmpl := newSite(t)
		tmplD := newScanner(t, tmpl)
		tripID, dispatch := w9cAlertingTrip(t, tmpl, tmplD)

		ack1 := w9cKey{origin, 1, 2} // site-a, after the dispatch
		retr1 := w9cKey{here, 1, 1}  // site-b retracts ack1
		ack2 := w9cKey{here, 1, 2}   // site-b's own fresh ack
		ack1Env := w9cAckEnvelope(ack1, tripID, nil, nil, "post "+origin, 1_700_000_300_000)
		// Earlier on the wall clock than BOTH acks it sits between.
		retrEnv := w9cRetractionEnvelope(t, tmpl, retr1, ack1, "post "+here, 1_700_000_100_000)
		ack2Env := w9cAckEnvelope(ack2, tripID, nil, nil, "post "+here, 1_700_000_900_000)

		streamA := []store.Envelope{dispatch, ack1Env}
		streamB := []store.Envelope{retrEnv, ack2Env}
		w9cAssertPermutationsAgree(t, tripID, dispatch, streamA, streamB,
			false, // ack2 is unretracted ⇒ SUPPRESSED
			"W9c-4 (three-event crossing): ack2 stands unretracted, so the trip must be suppressed in every arrival order")
	})
}

// w9cKeyLess is (site_id, node_epoch, seq) order — the ordering a
// key-precedence resolver would use, written out here so the fixture can
// assert which side of it each pair sits on.
func w9cKeyLess(a, b w9cKey) bool {
	if a.Site != b.Site {
		return a.Site < b.Site
	}
	if a.Epoch != b.Epoch {
		return a.Epoch < b.Epoch
	}
	return a.Seq < b.Seq
}

// w9cSplitStreams sorts hand-built envelopes onto their two site streams,
// preserving the given order within each.
func w9cSplitStreams(envs ...store.Envelope) map[string][]store.Envelope {
	out := map[string][]store.Envelope{origin: nil, here: nil}
	for _, e := range envs {
		out[e.SiteID] = append(out[e.SiteID], e)
	}
	return out
}

// w9cAssertPermutationsAgree delivers every interleaving of the two streams
// to a fresh store and asserts (a) byte-identical DumpProjections across
// all of them, (b) an identical banner across all of them, and (c) that the
// banner says what the criterion says it must.
func w9cAssertPermutationsAgree(t *testing.T, tripID string, dispatch store.Envelope, streamA, streamB []store.Envelope, wantAlerting bool, msg string) {
	t.Helper()
	orders := w9cInterleavings(streamA, streamB)
	if len(orders) < 2 {
		t.Fatalf("W9c-4 fixture: only %d delivery order(s) — 'in every permutation' asserts nothing over a single order", len(orders))
	}
	var firstDump, firstBanner, firstName string
	for i, order := range orders {
		name := w9cOrderName(order)
		st := newSite(t)
		d := newScanner(t, st)
		for _, e := range order {
			w9cApply(t, st, e)
			if e.SiteID == dispatch.SiteID && e.NodeEpoch == dispatch.NodeEpoch && e.Seq == dispatch.Seq {
				d.OnApplied(e, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary
			}
		}
		b := mustEval(t, d)

		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatalf("W9c-4: DumpProjections in order %s: %v", name, err)
		}
		banner := w9cBannerStatuses(b)
		if i == 0 {
			firstDump, firstBanner, firstName = string(dump), banner, name
		} else {
			if string(dump) != firstDump {
				t.Fatalf("W9c-4: DumpProjections is NOT byte-identical between delivery orders %s and %s — the fold is order-dependent (m3/m3b)", firstName, name)
			}
			if banner != firstBanner {
				t.Fatalf("W9c-4: Banner() differs between delivery orders %s (%s) and %s (%s) — the predicate consulted an order", firstName, firstBanner, name, banner)
			}
		}
		if got := w9cBannerHas(b, tripID); got != wantAlerting {
			t.Fatalf("%s — in delivery order %s the trip is alerting=%v, want %v (banner: %s)", msg, name, got, wantAlerting, banner)
		}
	}
}
