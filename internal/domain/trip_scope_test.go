// trip_scope_test.go — F28: the scoped, batched trip_state recompute.
//
// Three properties are held here, each against the wholesale recompute that
// remains in trip.go as the reference implementation:
//
//  1. WORK BOUND (the fail-before test): applying one more check-in pair must
//     touch a bounded number of rows, independent of how much unrelated
//     history the database holds. The wholesale per-event recompute rewrote
//     every derived row on every trip-family event — this test fails on that
//     code by three orders of magnitude, and is the regression tripwire that
//     keeps the quadratic from coming back.
//  2. BATCH-SHAPE INDEPENDENCE: the same event set applied per-event, as one
//     batch, or in any partition into batches yields byte-identical dumps.
//  3. DIFFERENTIAL EQUIVALENCE: after any random soup of trip-family events
//     has been applied through the scoped path, running the wholesale
//     reference recompute over the final raw tables changes NOTHING — i.e.
//     the scoped path left exactly the pure-function-of-tables state. If the
//     scoping invariant were unsound anywhere (a key's outcome changed by an
//     event under another key that the scope missed), the wholesale pass
//     would correct the stale row and the dumps would differ.
package domain

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// registerTotalChangesProbe registers a view over SQLite's total_changes()
// so a test can count rows written through the store's own connection —
// deterministic, no timing. Test-only; never part of the real registration.
func registerTotalChangesProbe(t *testing.T, st *store.Store) func() int64 {
	t.Helper()
	if err := st.RegisterProjection(store.Projection{
		Name:    "zz_test_total_changes",
		Table:   "zz_test_total_changes",
		OrderBy: "n",
		Schema:  `CREATE VIEW IF NOT EXISTS zz_test_total_changes (n) AS SELECT total_changes();`,
	}); err != nil {
		t.Fatalf("register probe: %v", err)
	}
	return func() int64 {
		b, err := st.DumpProjection("zz_test_total_changes")
		if err != nil {
			t.Fatalf("read probe: %v", err)
		}
		var rows []struct {
			N int64 `json:"n"`
		}
		if err := json.Unmarshal(b, &rows); err != nil || len(rows) != 1 {
			t.Fatalf("probe shape: %v %s", err, b)
		}
		return rows[0].N
	}
}

// TestApplyWorkIsBoundedRegardlessOfSeasonSize is the F28 fail-before test:
// on the wholesale per-event recompute the delta below is ~2×(seeded rows)
// per event (measured ≈2,400+ at this seeding); scoped it is a couple dozen.
func TestApplyWorkIsBoundedRegardlessOfSeasonSize(t *testing.T) {
	w := newC6World(t)
	changes := registerTotalChangesProbe(t, w.a)
	const seeded = 300
	for i := 0; i < seeded; i++ {
		w.checkInAtA()
	}
	before := changes()
	w.checkInAtA() // one more ordinary degenerate check-in pair (two events)
	delta := changes() - before
	// One pair writes: 2 log rows, trip+occupants+arrival+visits+visit
	// closes, 2 watermark upserts, and a point-scoped recompute of its own
	// trip_state/arrival_state rows. Generous bound, still 40× below what
	// one wholesale recompute of the seeded rows would cost.
	const bound = 60
	if delta > bound {
		t.Fatalf("one check-in pair changed %d rows with %d unrelated pairs seeded (bound %d) — apply cost is scaling with season size again (F28 regression)", delta, seeded, bound)
	}
}

// TestAuthoringStoreDerivedStateIsCurrent pins the Author path: Author runs
// the same batch Finalize as Apply (a batch of one), so the AUTHORING store's
// derived state is current the moment Author returns. This assertion exists
// because it is the one this file's other tests cannot make: they replay
// extracted streams through Apply/ApplyBatch, so an Author that silently
// skipped Finalize would keep them all green while every live authoring
// surface (check-in, unsolicited arrival, cancel) served stale derived state
// — exactly the gap that bit during this fix's landing (caught then by the
// web suite, pinned here at the layer that owns it).
func TestAuthoringStoreDerivedStateIsCurrent(t *testing.T) {
	w := newC6World(t)
	dst := "site-b"
	dl := int64(45)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED. Re-seeding the TRIP half
		// makes this fixture's arrival assertion FALSE, not merely uncompilable.
		// `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s FOURTH POSITIONAL PARAMETER from
		// the number-as-typed to a vehicle RECORD id WITHOUT changing arity or type
		// (tripauthor.go, its own ⟦was: "…, vehicleEntered, originSite string, …"⟧), so the
		// unsolicited call in this fixture still COMPILES while passing a number.
		// ⚠ MEASURED on production code alone, no test file involved:
		//     number as the 4th arg -> vehicle_id="TRUCK 12"  snapshot=<nil>  disposition=PLAIN
		//     record id as the 4th arg -> vehicle_id="site-b-vehicle-…"  snapshot="TRUCK 12"  CORRELATED
		// `vehicleNumberSnapshot` finds no record for the number, so the arrival's snapshot is
		// NIL and it contributes no matching key (trip.go's own comment does the rest).
		// Repairing the arrival half is ONE root cause across 27 call sites and is the PLAN
		// OWNER's. See reports/window-build/RESEED-164.md §3.7.
		// ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, w.a, trip.TripID); st.State != TripStateOpen {
		t.Fatalf("authoring store: fresh dispatch state = %q, want open", st.State)
	}
	w.syncBFromA()
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "truck 12", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "truck 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	d := dispositionOf(t, w.b, arr.ArrivalID)
	if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
		t.Fatalf("authoring store: unsolicited disposition = %+v, want correlated to %s", d, trip.TripID)
	}
	if err := CancelTrip(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, w.a, trip.TripID); st.State != TripStateCancelled {
		t.Fatalf("authoring store: post-cancel state = %q, want cancelled", st.State)
	}
}

// scopeScenario authors a two-site scenario that exercises every correlation
// mechanism at once: two keys sharing a vehicle across case/whitespace, an
// ambiguous pair, an unsolicited bind, a cancel with a later unsolicited
// arrival (cancelled_match), explicit arrivals, degenerate check-ins, and a
// cross-referenced trip. Returns the two extracted streams.
// ⚠⚠ ⟦LOCK — §47 re-seed, 2026-09-03⟧ A MEASURED COST OF THE RE-SEED, FLAGGED RATHER
// THAN ABSORBED. The four dispatch seeds below now mint a vehicle RECORD each, so this
// scenario's own streams grew from (A=12, B=5) to (A=16, B=6). `interleavingsOf`
// MATERIALISES every interleaving before anything samples it, and C(17,5)=6,188 becomes
// C(22,6)=74,613 — MEASURED in isolation: live heap 33 MB -> 456 MB, total allocation
// 153 MB -> 3.0 GB. Nothing here changed meaning and no assertion moved; `maxOrders`
// still samples 40 afterwards. But this file's own comment is "keep the suite fast when
// streams multiply", and they just multiplied. ⚠
// TestBatchPartitioningIsDeliveryShapeIndependent does NOT go through
// replayAllOrders' sampling — it indexes `orders`
// directly, so it carries the full list. Raised for the plan owner; NOT tuned here,
// because changing maxOrders or the sampling is not a re-seed.
// See reports/window-build/RESEED-164.md §5.
// ⚠⚠ ⟦DISARMED IN PART — §47 re-seed, 2026-09-03⟧ THE FOUR SEEDS BELOW ARE RE-SEEDED AND
// THE SCENARIO NO LONGER DOES WHAT ITS OWN COMMENT SAYS. It claims to exercise "every
// correlation mechanism at once … an ambiguous pair, an unsolicited bind, a cancel with a
// later unsolicited arrival (cancelled_match)". ⚠ NONE OF THOSE THREE OUTCOMES OCCURS ANY
// MORE: `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s 4th positional parameter to a
// vehicle RECORD id without changing arity, so the three unsolicited calls below still
// compile while passing NUMBERS, and every one now lands PLAIN.
// ⚠ The consumers assert byte-identical dumps across delivery orders, so they still PASS —
// but over a far NARROWER state than the fixture was built to produce. A weakened check,
// not a false one, and recorded rather than absorbed.
// See reports/window-build/RESEED-164.md §3.9.
// ⭐ ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ RE-ARMED IN FULL. The two unsolicited calls
// below now pass vehicle RECORD ids, and ALL THREE outcomes the comment claims occur again
// [MEASURED, plant P4 — this scenario's exact authoring shape]:
//
//	"Truck 12" against two open TRUCK 12 trips -> ambiguous
//	"van 7"    against the cancelled VAN 7 trip -> cancelled_match
//
// plus the unsolicited bind that the ambiguous pair is built from. The scenario does what its
// own comment says once more, and the order-independence check is back over the full state.
// ⚠⚠ AND IT COSTS, MEASURABLY — the §47 note above understates it now. The two arrival seeds
// add TWO envelopes to B's stream, so the materialised interleavings go C(22,6)=6.19e4 ->
// C(24,8)=7.35e5. PRODUCED on TestBatchPartitioningIsDeliveryShapeIndependent, same box, same
// command, the arrival seeds the ONLY variable:
//
//	§47 only          11.7 s,  508 MB max RSS
//	§47 + §50 arrival 94.6 s, 4747 MB max RSS      (8.1x slower, 9.3x the memory)
//
// ⚠ It PASSES here on a 16 GB box; 4.7 GB for ONE test is a hazard on a smaller runner, and
// this test does NOT go through replayAllOrders' sampling — it indexes `orders` directly.
// Tuning `maxOrders` or the sampling is NOT a re-seed, so it is raised, not absorbed: the
// PLAN OWNER's. See reports/window-build/ARRIVAL-RESEED.md.
func scopeScenario(t *testing.T) (aEvents, bEvents []store.Envelope) {
	t.Helper()
	w := newC6World(t)
	// Ordinary degenerate check-ins at A (majority path).
	w.checkInAtA()
	w.checkInAtA()
	// Two open inter-site trips, same key after normalisation → ambiguous
	// for the first unsolicited arrival at B.
	dst := "site-b"
	ddl := int64(45)
	t1, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &ddl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "truck  12", DriverPersonID: w.passenger.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "truck  12"), DriverPersonID: w.passenger.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &ddl,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A second key: dispatched, then cancelled at A, then an unsolicited
	// arrival for it at B → cancelled_match.
	t3, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "VAN 7", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "VAN 7"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dst, DeadlineMinutes: &ddl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelTrip(w.a, "site-a", 1, t3.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// B: ambiguous unsolicited (two open TRUCK 12), cancelled_match (VAN 7),
	// an explicit arrival closing t1, and a degenerate check-in of B's own.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "Truck 12", "site-a","⟧
	if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "Truck 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "van 7", "site-a","⟧
	if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "van 7"), "site-a",
		[]string{w.passenger.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, t1.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	bt, err := DispatchLocalCheckIn(w.b, "site-b", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.b, "site-b", "gate walk-in"), DriverPersonID: w.driver.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, bt.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return w.events(w.a, "site-a"), w.events(w.b, "site-b")
}

// TestBatchPartitioningIsDeliveryShapeIndependent: same events, any batch
// partition, any cross-stream interleaving — byte-identical dumps.
func TestBatchPartitioningIsDeliveryShapeIndependent(t *testing.T) {
	aEvents, bEvents := scopeScenario(t)
	orders := interleavingsOf(aEvents, bEvents)
	// Per-event application of the first interleaving is the reference.
	ref := newDomainTestStore(t)
	for _, ev := range orders[0] {
		if _, err := ref.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	refDump, err := ref.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	sizes := []int{1, 2, 3, 5, len(orders[0])}
	sampled := []int{0, len(orders) / 2, len(orders) - 1}
	for _, oi := range sampled {
		order := orders[oi]
		for _, size := range sizes {
			st := newDomainTestStore(t)
			for i := 0; i < len(order); i += size {
				end := i + size
				if end > len(order) {
					end = len(order)
				}
				batch := append([]store.Envelope{}, order[i:end]...)
				if _, err := st.ApplyBatch(batch); err != nil {
					t.Fatalf("order %d size %d: %v", oi, size, err)
				}
			}
			dump, err := st.DumpProjections()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(refDump, dump) {
				t.Fatalf("order %d, batch size %d: dump diverges from per-event reference — batching is not shape-independent", oi, size)
			}
		}
	}
}

// soupEnvelope builds one raw envelope on a site's stream (the sanctioned
// injection pattern — Apply does not care who authored it).
func soupEnvelope(site string, seq int64, payload any) store.Envelope {
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return store.Envelope{
		SiteID: site, NodeEpoch: 1, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: site, Payload: b,
	}
}

// randomSoup generates two valid site streams of trip-family events designed
// to collide on correlation keys: a small vehicle pool with case/whitespace
// variants, degenerate, inter-site AND uncontrolled-destination trips,
// explicit and unsolicited arrivals (including uncorrelatable ones), and
// origin-authored cancels.
//
// Uncontrolled-destination dispatches (round-2 audit F5/N7b) are a THIRD
// shape, not a rename of "degenerate": both leave destination_site NULL, but
// only an uncontrolled run also sets destination_uncontrolled — the one
// field a wholesale-reference regression could COALESCE into candidacy
// without a degenerate trip ever tripping it. Without this shape in the
// soup, TestScopedRecomputeMatchesWholesaleOnRandomSoups's differential
// oracle could never observe that mutation: COALESCE(NULL, NULL) is still
// NULL for a degenerate trip.
func randomSoup(rng *rand.Rand, events int) (aEvents, bEvents []store.Envelope) {
	vehicles := []string{"TRUCK 12", "truck  12", "Truck 12 ", "VAN 7", "van 7", "LOADER-3"}
	sites := []string{"site-a", "site-b"}
	pads := []string{"conneco", "quaqua"}
	seq := map[string]int64{}
	next := func(site string) int64 { seq[site]++; return seq[site] }
	type tripInfo struct {
		id, origin   string
		degenerate   bool
		uncontrolled bool
	}
	var trips []tripInfo
	var out = map[string]*[]store.Envelope{"site-a": &aEvents, "site-b": &bEvents}
	person := func(i int) string { return fmt.Sprintf("p-%d", i) }
	for n := 0; n < events; n++ {
		site := sites[rng.Intn(2)]
		other := "site-a"
		if site == "site-a" {
			other = "site-b"
		}
		switch r := rng.Float64(); {
		case r < 0.45: // dispatch (degenerate, inter-site, or uncontrolled)
			id := fmt.Sprintf("t-%s-%d", site, len(trips))
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ trip.vehicle_entered is a
			// nullable derived snapshot under DR-W7-4, so the payload field is
			// a *string. The value drawn from the generator is unchanged.
			// ⟦was: "VehicleEntered: vehicles[rng.Intn(len(vehicles))],"⟧
			p := TripDispatched{
				Type: "trip_dispatched", TripID: id,
				VehicleEntered: strptr(vehicles[rng.Intn(len(vehicles))]),
				DriverPersonID: person(rng.Intn(6)),
			}
			var degenerate, uncontrolled bool
			switch shape := rng.Float64(); {
			case shape < 1.0/3.0:
				degenerate = true // neither field set
			case shape < 2.0/3.0:
				dl := int64(30 + rng.Intn(60))
				p.DestinationSite = &other
				p.DeadlineMinutes = &dl
			default:
				uncontrolled = true
				pad := pads[rng.Intn(len(pads))]
				p.DestinationUncontrolled = &pad // no deadline: mutually exclusive
			}
			if rng.Float64() < 0.5 {
				p.OccupantPersonIDs = []string{person(rng.Intn(6))}
			}
			*out[site] = append(*out[site], soupEnvelope(site, next(site), p))
			trips = append(trips, tripInfo{id: id, origin: site, degenerate: degenerate, uncontrolled: uncontrolled})
		case r < 0.65 && len(trips) > 0: // explicit arrival for a random known trip
			ti := trips[rng.Intn(len(trips))]
			aid := fmt.Sprintf("ar-%s-%d", site, next(site))
			p := TripArrival{
				Type: "trip_arrival", ArrivalID: aid, TripRef: &ti.id,
				Visits: []ArrivalVisit{{VisitID: aid + "-v0", PersonID: person(rng.Intn(6))}},
			}
			*out[site] = append(*out[site], soupEnvelope(site, seq[site], p))
		case r < 0.85: // unsolicited arrival (sometimes uncorrelatable)
			aid := fmt.Sprintf("au-%s-%d", site, next(site))
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
			*out[site] = append(*out[site], soupEnvelope(site, seq[site], p))
		case len(trips) > 0: // cancel — authored by the trip's ORIGIN site only
			ti := trips[rng.Intn(len(trips))]
			p := TripCancelled{Type: "trip_cancelled", TripID: ti.id}
			*out[ti.origin] = append(*out[ti.origin], soupEnvelope(ti.origin, next(ti.origin), p))
		default: // nothing eligible yet
		}
	}
	return aEvents, bEvents
}

// errProbeRollback is the sentinel that forces the wholesale-probe
// transaction to roll back after comparing — the probe must observe, never
// change, the store.
var errProbeRollback = errors.New("probe rollback (sentinel, not a failure)")

// assertDerivedIsWholesaleFixpoint runs the wholesale reference recompute
// inside a DELIBERATELY ROLLED-BACK transaction and asserts it changes
// nothing — i.e. the committed derived state is exactly the pure function of
// the raw tables at THIS point, not only at the end of a run. Adopted from
// the independent verification of this fix, which pointed out that a
// final-state-only differential cannot see a mid-run violation that a later
// batch happens to repair.
func assertDerivedIsWholesaleFixpoint(t *testing.T, st *store.Store, label string) {
	t.Helper()
	snapshot := func(tx *sql.Tx, table string) string {
		rows, err := tx.Query(`SELECT * FROM ` + table + ` ORDER BY 1`)
		if err != nil {
			t.Fatalf("%s: snapshot %s: %v", label, table, err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		var sb strings.Builder
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("%s: snapshot %s: %v", label, table, err)
			}
			fmt.Fprintf(&sb, "%v\n", vals)
		}
		return sb.String()
	}
	var before, after string
	err := st.LocalTx(func(tx *sql.Tx) error {
		before = snapshot(tx, "trip_state") + snapshot(tx, "arrival_state")
		if err := recomputeTripStates(tx); err != nil {
			return err
		}
		after = snapshot(tx, "trip_state") + snapshot(tx, "arrival_state")
		return errProbeRollback
	})
	if !errors.Is(err, errProbeRollback) {
		t.Fatalf("%s: probe: %v", label, err)
	}
	if before != after {
		t.Fatalf("%s: committed derived state is not the wholesale fixpoint of the raw tables — the scoped recompute left a stale row here\nbefore:\n%s\nafter:\n%s", label, before, after)
	}
}

// applySoup delivers both streams into st in a random cross-stream
// interleaving (in-stream order preserved), in random batch sizes, and —
// when perBatchOracle is set — proves derived = f(raw) after EVERY committed
// batch, not only at the end.
func applySoup(t *testing.T, st *store.Store, rng *rand.Rand, aEvents, bEvents []store.Envelope, perBatchOracle bool) {
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
	batch := 0
	for i := 0; i < len(merged); {
		size := 1 + rng.Intn(7)
		end := i + size
		if end > len(merged) {
			end = len(merged)
		}
		if _, err := st.ApplyBatch(append([]store.Envelope{}, merged[i:end]...)); err != nil {
			t.Fatalf("soup apply: %v", err)
		}
		if perBatchOracle {
			assertDerivedIsWholesaleFixpoint(t, st, fmt.Sprintf("batch %d (events %d..%d)", batch, i, end-1))
		}
		i = end
		batch++
	}
}

// TestScopedRecomputeMatchesWholesaleOnRandomSoups is the differential
// enforcement of the scoping invariant, plus cross-order determinism at fuzz
// scale: two stores fed the same soup in different interleavings/batchings
// must agree with each other AND with the wholesale reference recompute run
// over the final raw tables.
func TestScopedRecomputeMatchesWholesaleOnRandomSoups(t *testing.T) {
	for seed := int64(1); seed <= 12; seed++ {
		rng := rand.New(rand.NewSource(seed))
		aEvents, bEvents := randomSoup(rng, 120)
		st1 := newDomainTestStore(t)
		st2 := newDomainTestStore(t)
		applySoup(t, st1, rand.New(rand.NewSource(seed*31+1)), aEvents, bEvents, true)
		applySoup(t, st2, rand.New(rand.NewSource(seed*31+2)), aEvents, bEvents, false)
		d1, err := st1.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		d2, err := st2.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(d1, d2) {
			t.Fatalf("seed %d: two delivery shapes of the same soup diverge — order/batch dependence in the scoped fold", seed)
		}
		// Differential: the wholesale reference over the final raw tables
		// must change nothing the scoped path left behind.
		if err := st1.LocalTx(func(tx *sql.Tx) error { return recomputeTripStates(tx) }); err != nil {
			t.Fatal(err)
		}
		dRef, err := st1.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(d1, dRef) {
			t.Fatalf("seed %d: wholesale reference recompute changed derived state the scoped path left — the scoping invariant is violated somewhere in this soup", seed)
		}
	}
}
