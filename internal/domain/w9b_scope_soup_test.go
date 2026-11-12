// w9b_scope_soup_test.go — W9b-6 (F4 §12 T17): the differential guard learns
// the new event.
//
// THE CRITERION, and the one place this file departs from its letter.
// W9b-6 says `randomSoup`'s event vocabulary "gains `arrival_ref_retracted`"
// and that `TestScopedRecomputeMatchesWholesaleOnRandomSoups` "passes with it
// in the vocabulary". Both of those names belong to `trip_scope_test.go`, an
// EXISTING test file, and this test-writer's scope forbids modifying an
// existing test. So the property is carried here, in W9b's own file, over a
// five-shape soup generator (`w9bSoup`) that is the four-shape vocabulary
// plus the retraction, and the same two-part differential assertion. ⚠ THE
// EDIT TO `randomSoup` ITSELF IS STILL OWED — it is a BUILDER edit to an
// existing test file, it is reported as such in
// `reports/window-build/W9b-tests.md`, and W9b's gate should book it. This
// file makes the property CHECKED; it does not make the criterion's literal
// instruction DONE.
//
// ⚠ WHY THE VOCABULARY ALONE IS NOT COVERAGE, and why this file's real
// content is the non-degeneracy arm. A soup that merely CONTAINS
// `arrival_ref_retracted` events is, on any build that ignores them, exactly
// the soup without them: the scoped path and the wholesale reference both
// ignore the event, they agree with each other perfectly, and the differential
// oracle is green while covering nothing. That is the un-controlled green
// W6's gate blocked a ledger on. So the load-bearing assertion here is the
// TWIN SOUP: the identical event set with every retraction replaced by an
// inert filler at the same key must produce DIFFERENT derived state. If it
// does not, the retractions did nothing and the differential proved nothing.
//
// ⟦NO PRE-CHUNK RED — the "and it passes" half⟧, per W9b-6's own text:
// `TestScopedRecomputeMatchesWholesaleOnRandomSoups` is green on the
// delivered tree today, so a clean green here is not evidence either. The
// evidence is (i) the twin-soup arm, which REDS pre-chunk, and (ii) the
// standing too-narrow-scope control — mutant m25 — which cannot be produced
// before the scope entry exists and is booked to W9b's gate. See the report
// for the exact plant and for the ANALOGUE control that was produced today.
//
// KILLER MUTANT: m25 — the too-narrow retraction scope. The scoped fold binds
// under a corrKey that was never scoped and disagrees with the wholesale fold
// on the TYPICAL mis-click, surfacing as a wrong `arrival_state` at one post
// only. ⚠ W9b-1's order-independence half cannot substitute: both permutations
// run through the SAME scoped path, so a uniformly-too-narrow scope is
// byte-identically too narrow in both and `DumpProjections` agrees with itself.
package domain

import (
	"database/sql"
	"fmt"
	"math/rand"
	"testing"

	"sitecontrol/internal/store"
)

// w9bSoupFillerType is the inert stand-in the twin soup substitutes for each
// retraction. It matches no projection, so it folds nowhere and changes
// nothing — which is the point: it holds the stream's seq continuity (the
// store refuses a gap, §8.4) while removing the retraction's effect.
const w9bSoupFillerType = "w9b_soup_filler"

// w9bSoup generates two valid site streams over the FIVE-shape vocabulary:
// the four `randomSoup` already has — dispatch (degenerate / inter-site /
// uncontrolled), explicit arrival, unsolicited arrival (sometimes
// uncorrelatable), origin-authored cancel — plus `arrival_ref_retracted`
// naming an explicit arrival already in the soup.
//
// It returns the two streams AND a parallel pair of streams in which every
// retraction is replaced, at the same key and with the same envelope
// metadata, by an inert filler. The two sets are generated in one pass so
// they cannot drift apart.
func w9bSoup(rng *rand.Rand, events int) (aEvents, bEvents, aFiller, bFiller []store.Envelope, retractions int) {
	vehicles := []string{"TRUCK 12", "truck  12", "Truck 12 ", "VAN 7", "van 7", "LOADER-3"}
	sites := []string{"site-a", "site-b"}
	pads := []string{"conneco", "quaqua"}
	seq := map[string]int64{}
	next := func(site string) int64 { seq[site]++; return seq[site] }
	type tripInfo struct {
		id, origin string
		degenerate bool
	}
	var trips []tripInfo
	var explicitArrivals []string
	out := map[string]*[]store.Envelope{"site-a": &aEvents, "site-b": &bEvents}
	fill := map[string]*[]store.Envelope{"site-a": &aFiller, "site-b": &bFiller}
	person := func(i int) string { return fmt.Sprintf("p-%d", i) }

	// emit appends the SAME envelope to both the live and the filler stream.
	emit := func(site string, s int64, payload any) {
		env := w9bEnvelope(site, 1, s, w9bBase, payload)
		*out[site] = append(*out[site], env)
		*fill[site] = append(*fill[site], env)
	}
	// emitRetraction appends the retraction to the live stream and the inert
	// filler — same site, same seq, same envelope metadata — to the twin.
	emitRetraction := func(site string, s int64, arrivalID string) {
		*out[site] = append(*out[site], w9bEnvelope(site, 1, s, w9bBase,
			w9bRetraction(arrivalID, w9bStr("soup"))))
		*fill[site] = append(*fill[site], w9bEnvelope(site, 1, s, w9bBase,
			map[string]any{"type": w9bSoupFillerType, "arrival_id": arrivalID}))
		retractions++
	}

	for n := 0; n < events; n++ {
		site := sites[rng.Intn(2)]
		other := "site-a"
		if site == "site-a" {
			other = "site-b"
		}
		switch r := rng.Float64(); {
		case r < 0.40: // dispatch (degenerate, inter-site, or uncontrolled)
			id := fmt.Sprintf("t-%s-%d", site, len(trips))
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧
			// ⟦was: "VehicleEntered: vehicles[rng.Intn(len(vehicles))],"⟧
			p := TripDispatched{
				Type: "trip_dispatched", TripID: id,
				VehicleEntered: strptr(vehicles[rng.Intn(len(vehicles))]),
				DriverPersonID: person(rng.Intn(6)),
			}
			degenerate := false
			switch shape := rng.Float64(); {
			case shape < 1.0/3.0:
				degenerate = true // neither field set
			case shape < 2.0/3.0:
				dl := int64(30 + rng.Intn(60))
				p.DestinationSite = &other
				p.DeadlineMinutes = &dl
			default:
				pad := pads[rng.Intn(len(pads))]
				p.DestinationUncontrolled = &pad // no deadline: mutually exclusive
			}
			if rng.Float64() < 0.5 {
				p.OccupantPersonIDs = []string{person(rng.Intn(6))}
			}
			emit(site, next(site), p)
			trips = append(trips, tripInfo{id: id, origin: site, degenerate: degenerate})
		case r < 0.60 && len(trips) > 0: // explicit arrival for a random known trip
			ti := trips[rng.Intn(len(trips))]
			s := next(site)
			aid := fmt.Sprintf("ar-%s-%d", site, s)
			emit(site, s, TripArrival{
				Type: "trip_arrival", ArrivalID: aid, TripRef: &ti.id,
				Visits: []ArrivalVisit{{VisitID: aid + "-v0", PersonID: person(rng.Intn(6))}},
			})
			explicitArrivals = append(explicitArrivals, aid)
		case r < 0.78: // unsolicited arrival (sometimes uncorrelatable)
			s := next(site)
			aid := fmt.Sprintf("au-%s-%d", site, s)
			p := TripArrival{
				Type: "trip_arrival", ArrivalID: aid,
				Visits: []ArrivalVisit{{VisitID: aid + "-v0", PersonID: person(rng.Intn(6))}},
			}
			if rng.Float64() < 0.85 {
				v := vehicles[rng.Intn(len(vehicles))]
				o := sites[rng.Intn(2)]
				p.VehicleEntered = &v
				p.OriginSite = &o
			}
			emit(site, s, p)
		case r < 0.88 && len(explicitArrivals) > 0:
			// THE FIFTH SHAPE — the retraction, naming an explicit arrival
			// this soup already contains. Authored at EITHER post (F4 §3.6:
			// "either post — the mistake may be discovered at the origin").
			emitRetraction(site, next(site), explicitArrivals[rng.Intn(len(explicitArrivals))])
		case len(trips) > 0: // cancel — authored by the trip's ORIGIN site only
			ti := trips[rng.Intn(len(trips))]
			emit(ti.origin, next(ti.origin), TripCancelled{Type: "trip_cancelled", TripID: ti.id})
		default: // nothing eligible yet
		}
	}
	return aEvents, bEvents, aFiller, bFiller, retractions
}

// w9bApplySoup interleaves two streams at random and applies them in random
// batches — W9b's own copy of the delivery shaping, so this test never breaks
// because another chunk's helper moved.
func w9bApplySoup(t *testing.T, st *store.Store, rng *rand.Rand, aEvents, bEvents []store.Envelope) {
	t.Helper()
	var merged []store.Envelope
	ai, bi := 0, 0
	for ai < len(aEvents) || bi < len(bEvents) {
		if bi >= len(bEvents) || (ai < len(aEvents) && rng.Intn(2) == 0) {
			merged = append(merged, aEvents[ai])
			ai++
		} else {
			merged = append(merged, bEvents[bi])
			bi++
		}
	}
	for i := 0; i < len(merged); {
		size := 1 + rng.Intn(7)
		end := i + size
		if end > len(merged) {
			end = len(merged)
		}
		if _, err := st.ApplyBatch(append([]store.Envelope{}, merged[i:end]...)); err != nil {
			t.Fatalf("w9b soup apply: %v", err)
		}
		i = end
	}
}

// w9bDerivedState is the pair of tables the scope entry can get wrong —
// `trip_state` and `arrival_state`. The twin-soup comparison is made over
// these two and not over `DumpProjections`, because the inert filler occupies
// a log slot the retraction would occupy and the raw log is not derived state.
func w9bDerivedState(t *testing.T, st *store.Store) string {
	t.Helper()
	return w9bSection(t, st, "trip_state") + "\n" + w9bSection(t, st, "arrival_state")
}

// TestW9b6ScopedRecomputeMatchesWholesaleWithRetractionsInTheVocabulary is
// W9b-6 in full.
//
// Three assertions per seed, in this order, because the later ones are
// meaningless without the earlier:
//
//  1. THE SOUP CONTAINS RETRACTIONS (fixture non-degeneracy — a generator
//     whose fifth case never fired would make everything below vacuous);
//  2. THE RETRACTIONS ARE LOAD-BEARING (the twin soup diverges) — this is
//     the arm that REDS on the pre-chunk tree, and the arm that stops the
//     differential being an un-controlled green;
//  3. THE DIFFERENTIAL HOLDS: two delivery shapes of the same soup agree with
//     each other, and the wholesale reference recompute over the final raw
//     tables changes nothing the scoped path left behind.
func TestW9b6ScopedRecomputeMatchesWholesaleWithRetractionsInTheVocabulary(t *testing.T) {
	totalRetractions := 0
	divergedOnSomeSeed := false

	for seed := int64(1); seed <= 8; seed++ {
		rng := rand.New(rand.NewSource(seed))
		aEvents, bEvents, aFiller, bFiller, retractions := w9bSoup(rng, 120)
		totalRetractions += retractions
		if retractions == 0 {
			continue // this seed's fifth case never fired; the totals gate below catches a generator that never fires at all
		}

		st1 := w9bNewStore(t)
		st2 := w9bNewStore(t)
		w9bApplySoup(t, st1, rand.New(rand.NewSource(seed*31+1)), aEvents, bEvents)
		w9bApplySoup(t, st2, rand.New(rand.NewSource(seed*31+2)), aEvents, bEvents)

		// --- (3a) two delivery shapes of the same soup agree ------------
		d1 := w9bAllProjections(t, st1)
		if d2 := w9bAllProjections(t, st2); d1 != d2 {
			t.Fatalf("W9b-6, seed %d: two delivery shapes of the same soup diverge — order/batch dependence in the scoped fold once `%s` is in the vocabulary",
				seed, w9bEvtRefRetracted)
		}

		// --- (3b) the wholesale reference changes nothing ---------------
		if err := st1.LocalTx(func(tx *sql.Tx) error { return recomputeTripStates(tx) }); err != nil {
			t.Fatal(err)
		}
		if dRef := w9bAllProjections(t, st1); d1 != dRef {
			t.Fatalf("W9b-6 (m25 — the too-narrow retraction scope), seed %d: the wholesale reference recompute CHANGED derived state the scoped path left behind.\n"+
				"F4 §3.6: the retraction's scope entry must point-scope the named arrival, AND its own (origin_claim, vehicle) corrKey where it has one, AND the referenced trip — three hops, not one. "+
				"A scope narrower than that binds under a corrKey never scoped and disagrees with the wholesale fold on the typical mis-click, surfacing as a wrong `arrival_state` at ONE POST ONLY",
				seed)
		}

		// --- (2) the twin soup: are the retractions load-bearing? -------
		stTwin := w9bNewStore(t)
		w9bApplySoup(t, stTwin, rand.New(rand.NewSource(seed*31+1)), aFiller, bFiller)
		if w9bDerivedState(t, st2) != w9bDerivedState(t, stTwin) {
			divergedOnSomeSeed = true
		}
	}

	if totalRetractions == 0 {
		t.Fatalf("W9b-6 FIXTURE DEFECT: `w9bSoup` emitted ZERO `%s` events across every seed — the vocabulary did not actually gain the fifth shape and everything this test asserts is vacuous",
			w9bEvtRefRetracted)
	}

	// --- THE CONTROLLED ASSERTION ---------------------------------------
	if !divergedOnSomeSeed {
		t.Fatalf("W9b-6: %d `%s` events were applied across the soups and the derived state (`trip_state` + `arrival_state`) is BYTE-IDENTICAL to the same soups with every retraction replaced by an inert filler.\n"+
			"The event is in the vocabulary and the fold does not know it, so the differential oracle above agrees with itself while covering NOTHING — the un-controlled green this criterion exists to forbid. "+
			"F4 §3.6: `randomSoup`'s switch \"will never emit an %s unless a builder extends it, so it keeps passing and covers nothing about this event\"",
			totalRetractions, w9bEvtRefRetracted, w9bEvtRefRetracted)
	}
}
