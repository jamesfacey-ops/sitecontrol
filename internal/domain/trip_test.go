// trip_test.go — table-driven fold tests for the C6 trip lifecycle, each
// scenario applied in PERMUTED cross-stream delivery orders (chunk-plan M4:
// "injection only substitutes for the real engine if delivery order is
// permuted"). Every scenario's events are authored through the REAL
// authoring APIs on scratch stores, extracted as per-site streams, and then
// replayed into blank stores in every enumerated interleaving (in-stream
// order is fixed by the watermark's contiguity rule; ACROSS streams anything
// goes). Each order must produce a byte-identical projection dump AND pass
// the scenario's own assertions — the done-when's "fold output is identical
// for the same event set regardless of delivery order", measured, not
// assumed. (The other half of that criterion — the same property observed
// across two real instances via C3a + dump-compare — is the chunk's verify
// edge, exercised on the C4 environment, not from this unit suite.)
package domain

import (
	"bytes"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- harness ---

// interleavingsOf enumerates every merge of streams a and b that preserves
// each stream's internal order. Scenario streams here are small (the largest
// is ~12×2), so full enumeration stays in the dozens.
func interleavingsOf(a, b []store.Envelope) [][]store.Envelope {
	if len(a) == 0 {
		return [][]store.Envelope{append([]store.Envelope{}, b...)}
	}
	if len(b) == 0 {
		return [][]store.Envelope{append([]store.Envelope{}, a...)}
	}
	var out [][]store.Envelope
	for _, rest := range interleavingsOf(a[1:], b) {
		out = append(out, append([]store.Envelope{a[0]}, rest...))
	}
	for _, rest := range interleavingsOf(a, b[1:]) {
		out = append(out, append([]store.Envelope{b[0]}, rest...))
	}
	return out
}

const maxOrders = 40 // keep the suite fast when streams multiply

// replayAllOrders applies every interleaving of aEvents/bEvents to a fresh
// blank store, asserts all dumps are byte-identical, and runs check against
// EVERY order's final state — a scenario assertion that only held for one
// delivery order would fail here by construction.
func replayAllOrders(t *testing.T, aEvents, bEvents []store.Envelope, check func(t *testing.T, st *store.Store)) {
	t.Helper()
	orders := interleavingsOf(aEvents, bEvents)
	if len(orders) < 2 {
		t.Fatalf("scenario has only %d delivery order(s) — permutation requires at least 2 (M4); give it a second stream", len(orders))
	}
	if len(orders) > maxOrders {
		// Keep the extremes (all-A-first, all-B-first are always the first
		// and last enumerated) plus an evenly-spaced sample between.
		sampled := [][]store.Envelope{orders[0]}
		step := len(orders) / (maxOrders - 2)
		for i := step; i < len(orders)-1; i += step {
			sampled = append(sampled, orders[i])
		}
		sampled = append(sampled, orders[len(orders)-1])
		orders = sampled
	}
	var refDump []byte
	for i, order := range orders {
		st := newDomainTestStore(t)
		for _, ev := range order {
			if _, err := st.Apply(ev); err != nil {
				t.Fatalf("order %d: apply %v: %v", i, ev.Key(), err)
			}
		}
		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatalf("order %d: dump: %v", i, err)
		}
		if i == 0 {
			refDump = dump
		} else if !bytes.Equal(refDump, dump) {
			t.Fatalf("order %d: dump diverges from order 0 — the fold is delivery-order-dependent\nref: %s\ngot: %s", i, refDump, dump)
		}
		check(t, st)
	}
}

// c6World is a scenario workbench: a site-a authoring store, a site-b
// authoring store, and a small roster (company/workgroup/two people) seeded
// at site-a. Streams are extracted with events().
type c6World struct {
	t         *testing.T
	a, b      *store.Store
	company   Company
	trade     Trade
	driver    Person
	passenger Person
}

func newC6World(t *testing.T) *c6World {
	t.Helper()
	w := &c6World{t: t, a: newDomainTestStore(t), b: newDomainTestStore(t)}
	var err error
	if w.company, err = CreateCompany(w.a, "site-a", 1, "Kakivik Asset Management", 0, "default"); err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §60, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a
	// site-wide open set, not scoped to a company. ⚠ THIS IS THE MECHANICAL DROP, NOT ONE OF
	// §60's ROUTED ARITY SITES, and that was VERIFIED HERE rather than taken on trust:
	//   • `CreateTrade(st, siteID, nodeEpoch, name, occurredAtMS, timeSource)` (refdata.go:553)
	//     no longer takes a company, and `Trade` (refdata.go:202) carries `SiteID` — never a
	//     `CompanyID`. There is no company↔trade association left in the type to preserve.
	//   • This is a FIXTURE BUILDER, not an assertion — `newC6World`.
	//   • ⚠ THE TEST FOR "is a distinction lost": does anything here distinguish trades BY
	//     COMPANY? [MEASURED] No. `w.trade` is read at exactly two places — `CreatePerson`
	//     (:112, :115), which takes the company and the trade as SEPARATE arguments so the
	//     person→company link survives untouched; and the visit-snapshot assertions (:317-324),
	//     which check `CompanySnapshotID` and `TradeSnapshotID` INDEPENDENTLY and never their
	//     pairing. The other `CreateCompany` calls (:242, :352, :757) build an unrelated company
	//     on site-b's store to keep those snapshots non-vacuous; there is no second trade.
	// So ⟦was: "TestTradeUniqueOnCompanyPlusName"⟧'s fate — §60's Kind A fossil — is not this site's:
	// nothing here asserts company-scoped trade uniqueness.
	// ⟦was: "if w.trade, err = CreateTrade(w.a, \"site-a\", 1, w.company.CompanyID, \"Inspection\", 0, \"default\"); err != nil {"⟧
	if w.trade, err = CreateTrade(w.a, "site-a", 1, "Inspection", 0, "default"); err != nil {
		t.Fatal(err)
	}
	if w.driver, err = CreatePerson(w.a, "site-a", 1, "Dana Driver", &w.company.CompanyID, &w.trade.TradeID, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if w.passenger, err = CreatePerson(w.a, "site-a", 1, "Pat Passenger", &w.company.CompanyID, &w.trade.TradeID, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return w
}

// events extracts a site's authored stream.
func (w *c6World) events(st *store.Store, siteID string) []store.Envelope {
	w.t.Helper()
	evs, err := st.Events(siteID, 1, 1, 0)
	if err != nil {
		w.t.Fatal(err)
	}
	return evs
}

// syncBFromA replays site-a's current stream into the site-b authoring store
// so B-side authoring (expected arrivals, roster lookups) can see it.
func (w *c6World) syncBFromA() {
	w.t.Helper()
	for _, ev := range w.events(w.a, "site-a") {
		if _, err := w.b.Apply(ev); err != nil {
			w.t.Fatal(err)
		}
	}
}

// checkInAtA drives the DEGENERATE LOCAL TRIP (design §10) for both roster
// people at site-a: a no-destination/no-deadline trip plus a same-site
// expected arrival — the ordinary gate check-in IS this machinery (C8's
// check-in form authors exactly this pair; C7's deadline scanner and replay
// gate skip it because DeadlineMinutes is nil).
func (w *c6World) checkInAtA() {
	w.t.Helper()
	trip, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(w.t, w.a, "site-a", "gate walk-in"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
	}, 0, "default")
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		w.t.Fatal(err)
	}
}

func openVisitsAt(t *testing.T, st *store.Store, siteID string) []Visit {
	t.Helper()
	visits, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []Visit
	for _, v := range visits {
		if v.SiteID == siteID && v.IsOpen == 1 {
			out = append(out, v)
		}
	}
	return out
}

func stateOf(t *testing.T, st *store.Store, tripID string) TripState {
	t.Helper()
	states, err := TripStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.TripID == tripID {
			return s
		}
	}
	t.Fatalf("no trip_state row for %s", tripID)
	return TripState{}
}

func dispositionOf(t *testing.T, st *store.Store, arrivalID string) ArrivalState {
	t.Helper()
	states, err := ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.ArrivalID == arrivalID {
			return s
		}
	}
	t.Fatalf("no arrival_state row for %s", arrivalID)
	return ArrivalState{}
}

func TestNormalizeVehicleID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"TRUCK 12", "truck 12"},
		{"  TRUCK   12", "truck 12"},
		{"truck 12 ", "truck 12"},
		{"\tTrUcK\t12\n", "truck 12"},
		{"   ", ""},
	} {
		if got := NormalizeVehicleID(tc.in); got != tc.want {
			t.Errorf("NormalizeVehicleID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- done-when: {trip} alone (Cm-6 rider) ---

// Dispatch alone — no arrival, no cancel anywhere in the event set — checks
// its occupants out of the origin site's on-site list, and the driver is an
// occupant row.
func TestDispatchAloneChecksOccupantsOutOfOriginOnSiteList(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA() // both people on site-a's on-site list first
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Second stream so delivery order is genuinely permutable (M4): site-b
	// authors unrelated reference data.
	if _, err := CreateCompany(w.b, "site-b", 1, "Unrelated Co", 0, "default"); err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		if got := openVisitsAt(t, st, "site-a"); len(got) != 0 {
			t.Fatalf("dispatch left %d open visit(s) at the origin — occupants were not checked out", len(got))
		}
		if s := stateOf(t, st, trip.TripID); s.State != TripStateOpen {
			t.Fatalf("trip state = %s, want open ({trip} alone)", s.State)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 0 {
			t.Fatalf("{trip} alone created %d visit(s) at the destination — nothing may arrive people early", len(got))
		}
		occupants, err := TripOccupants(st)
		if err != nil {
			t.Fatal(err)
		}
		driverIsOccupant := false
		for _, o := range occupants {
			if o.TripID == trip.TripID && o.PersonID == w.driver.PersonID {
				driverIsOccupant = true
			}
		}
		if !driverIsOccupant {
			t.Fatal("driver has no occupant row (§7: a label-only driver would vanish from every muster)")
		}
	})
}

// --- done-when: {trip, arrival} ---

// An expected arrival closes the fold's open expectation and creates one
// visit per occupant — the DRIVER INCLUDED — each carrying the point-in-time
// company/workgroup snapshot (8.8-i) as authored, in every delivery order.
func TestTripThenExpectedArrivalArrivesWithOneSnapshotVisitPerOccupant(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA()
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	arr, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrived {
			t.Fatalf("trip state = %s, want arrived", s.State)
		}
		if d := dispositionOf(t, st, arr.ArrivalID); d.Disposition != ArrivalExpected {
			t.Fatalf("arrival disposition = %s, want expected", d.Disposition)
		}
		visitsB := openVisitsAt(t, st, "site-b")
		if len(visitsB) != 2 {
			t.Fatalf("arrival created %d open visit(s) at site-b, want 2 (one per occupant)", len(visitsB))
		}
		byPerson := map[string]Visit{}
		for _, v := range visitsB {
			byPerson[v.PersonID] = v
		}
		dv, ok := byPerson[w.driver.PersonID]
		if !ok {
			t.Fatal("the driver is absent from the visit set (not-done-if)")
		}
		for _, v := range []Visit{dv, byPerson[w.passenger.PersonID]} {
			if v.CompanySnapshotID == nil || *v.CompanySnapshotID != w.company.CompanyID {
				t.Fatalf("visit %s company snapshot id = %v, want the roster reference actually used (%s)", v.VisitID, v.CompanySnapshotID, w.company.CompanyID)
			}
			if v.CompanySnapshotName == nil || *v.CompanySnapshotName != w.company.Name {
				t.Fatalf("visit %s company snapshot name = %v, want denormalised %q (8.8-i)", v.VisitID, v.CompanySnapshotName, w.company.Name)
			}
			if v.TradeSnapshotID == nil || *v.TradeSnapshotID != w.trade.TradeID {
				t.Fatalf("visit %s workgroup snapshot id = %v, want %s", v.VisitID, v.TradeSnapshotID, w.trade.TradeID)
			}
		}
	})
}

// --- done-when: {trip, cancel} ---

// A cancel closes the expectation and REINSTATES NO ONE: the occupants were
// checked out at dispatch and stay checked out (§9.1 — a cancel can also
// mean "went elsewhere").
func TestTripThenCancelIsCancelledAndReinstatesNoOne(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA()
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelTrip(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCompany(w.b, "site-b", 1, "Unrelated Co", 0, "default"); err != nil {
		t.Fatal(err) // second stream for permutation
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateCancelled {
			t.Fatalf("trip state = %s, want cancelled", s.State)
		}
		if got := openVisitsAt(t, st, "site-a"); len(got) != 0 {
			t.Fatalf("cancel reinstated %d visit(s) at the origin — a cancel must never touch visit state", len(got))
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 0 {
			t.Fatalf("cancelled trip has %d visit(s) at the destination", len(got))
		}
	})
}

// --- done-when: {trip, cancel, arrival} → review lane ---

// Cancel and arrival BOTH present — in any delivery order, including cancel
// delivered after the arrival — is arrived_after_cancel, a review state for
// a human, never auto-resolved. The arrival's visits stand (the people are
// physically on site) and the cancel still reinstates nobody at the origin.
func TestTripCancelArrivalLandsInReviewLaneNeverAutoResolves(t *testing.T) {
	w := newC6World(t)
	w.checkInAtA()
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// B hears only the trip, then checks the vehicle in; A cancels without
	// having heard the arrival — the partition case §9.1 is written for.
	w.syncBFromA()
	arr, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelTrip(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrivedAfterCancel {
			t.Fatalf("trip state = %s, want arrived_after_cancel (review)", s.State)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.ArrivedAfterCancel) != 1 || lane.ArrivedAfterCancel[0].TripID != trip.TripID {
			t.Fatalf("review lane does not surface the arrived-after-cancel trip: %+v", lane.ArrivedAfterCancel)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 2 {
			t.Fatalf("arrived-after-cancel has %d open visit(s) at site-b, want 2 — the people ARE there", len(got))
		}
		if got := openVisitsAt(t, st, "site-a"); len(got) != 0 {
			t.Fatalf("cancel reinstated %d visit(s) at the origin", len(got))
		}
		if d := dispositionOf(t, st, arr.ArrivalID); d.Disposition != ArrivalExpected {
			t.Fatalf("arrival disposition = %s, want expected", d.Disposition)
		}
	})
}

// --- done-when: unsolicited arrival binds to the oldest open trip ---

// Two trips for the same vehicle+origin exist, but only the OLDER one is
// still open (the newer was explicitly checked in). The unsolicited arrival
// binds the oldest OPEN trip — selection is over open trips ordered by the
// trip event's (node_epoch, seq) source key, never rowid or wall-time (row
// 10) — and the M8 rule holds: a "truck 12" arrival matches a "TRUCK 12"
// trip while both display their verbatim entered forms.
func TestUnsolicitedArrivalBindsOldestOpenTripAcrossCaseAndWhitespace(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(60)
	tripOld, err := Dispatch(w.a, "site-a", 1, DispatchInput{
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
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tripNew, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED — same cause as above:
		// the arrival half of this fixture passes a NUMBER where `W7-4(d)(i)` now wants a
		// vehicle RECORD id, so re-seeding the trip half makes the assertion FALSE.
		// ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.passenger.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.passenger.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, tripNew.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err) // the NEWER trip is explicitly checked in → closed
	}
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "truck 12", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "truck 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != tripOld.TripID {
			t.Fatalf("unsolicited arrival disposition = %+v, want correlated to the oldest OPEN trip %s", d, tripOld.TripID)
		}
		if s := stateOf(t, st, tripOld.TripID); s.State != TripStateArrived {
			t.Fatalf("oldest open trip state = %s, want arrived via correlation", s.State)
		}
		// Verbatim display forms (M8): the trip row holds exactly what A's
		// officer typed, the arrival row exactly what B's gate typed.
		trips, err := Trips(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range trips {
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "if tr.VehicleEntered != \"TRUCK 12\" {"⟧
			// An absent snapshot is not equal to the literal, so the assertion is unchanged.
			if tr.VehicleEntered == nil || *tr.VehicleEntered != "TRUCK 12" {
				t.Fatalf("trip vehicle stored as %q, want verbatim %q", derefString(tr.VehicleEntered), "TRUCK 12")
			}
		}
		arrivals, err := Arrivals(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range arrivals {
			if a.ArrivalID == arr.ArrivalID && (a.VehicleEntered == nil || *a.VehicleEntered != "truck 12") {
				t.Fatalf("arrival vehicle stored as %v, want verbatim %q", a.VehicleEntered, "truck 12")
			}
		}
	})
}

// --- done-when (M8, the storage half): normalisation NEVER reaches storage ---

// Entered forms are chosen so that NEITHER equals the normalised comparison
// form ("truck 12"). If any code path wrote the normalised string anywhere —
// a column, a derived table, anything the dump can see — the substring scan
// would find it and this test would fail. That is the not-done-if
// "normalisation rewrites the stored vehicle id", written to be falsifiable.
func TestNormalisedVehicleFormAppearsNowhereInStorage(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(60)
	// ⟦LOCK — §65, 2026-09-03⟧ B1 — THE TYPING MOMENT, which under DR-W7-4 is creation. The
	// operator types "  TRUCK   12"; the record keeps "TRUCK   12". Both halves of this test then
	// name THIS record, which is §50's ruling applied here: "because both halves name the same
	// vehicle record … the correlation the fixture asserts becomes true again, for the reason the
	// fixture always meant it to be true."
	paddedNumber := "  TRUCK   12"
	paddedVehicle, err := CreateVehicle(w.a, "site-a", 1, &paddedNumber, nil, 0, "default")
	if err != nil {
		t.Fatalf("fixture: the padded number must be ACCEPTED (only its padding is dropped): %v", err)
	}
	paddedVehicleID := paddedVehicle.VehicleID
	// ⭐ THE TRIMMING, ASSERTED RATHER THAN ASSUMED — this is what makes the needle below a
	// measured expectation instead of a guess about what the store did.
	if paddedVehicle.Number == nil || *paddedVehicle.Number != "TRUCK   12" {
		t.Fatalf("fixture: the record holds %q, want %q — leading/trailing space dropped, INTERNAL spacing kept",
			derefString(paddedVehicle.Number), "TRUCK   12")
	}
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⚠⚠ ⟦ROUTED BACK — §62, 2026-09-03⟧ STILL ROUTED, AND NOW ROUTED WITH A CLASS ANSWER:
		// THIS SITE FITS NONE OF R1/R2/R3. §62 bar 3 — "if it fits none, ROUTE IT BACK rather
		// than forcing the nearest fit" — so it is left uncompiled rather than repaired.
		//   • NOT R1: no guard moved. The trimming is `trimmedOrNil` (vehicle.go:258), which is
		//     DR-W7-15's deliberate normalisation, not a relocated refusal.
		//   • NOT R2: nothing became unconstructible. `"  TRUCK   12"` is still a number a
		//     record can hold — it is what STORAGE then holds that changed.
		//   • NOT R3: no measured window is involved. This test reads a projection dump, not an
		//     event count.
		// ⚠ WHAT IT IS: an assertion that has become FALSE. `bytes.Contains(dump, "  TRUCK   12")`
		// asserts "storage must hold exactly what was typed"; under the id door the record holds
		// the TRIMMED number and the derived snapshot loses the leading spaces. Repairing it
		// means either relaxing the verbatim claim or retiring the case — both plan-owner acts,
		// and §50 already names the trimming case in terms as one that STAYS ROUTED.
		// ⚠ ITS ARRIVAL HALF (below, `"Truck 12 "`) ROUTES WITH IT: the pair is one criterion
		// measured on both halves, and repairing one alone would leave the test asserting
		// something neither half means.
		//
		// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §65 B1⟧ NOT RE-SEEDED.
		// CreateVehicle applies trimmedOrNil (vehicle.go), so a record made from this literal
		// holds "TRUCK   12" and the derived trip.vehicle_entered loses the LEADING SPACES. This
		// test asserts `bytes.Contains(dump, "  TRUCK   12")` — "storage must hold exactly what
		// was typed". ⚠ That assertion becomes FALSE under the id door, not merely uncompilable.
		// PLAN OWNER's.
		//
		// ⟦LOCK — §65, 2026-09-03⟧ RE-EXPRESSED, NOT RETIRED. §65 B1: the criterion's real
		// content — storage holds what was typed, UNMANGLED — is still true of everything except
		// leading and trailing whitespace, so the needle keeps its INTERNAL spacing and gives up
		// only the padding. ⚠ The padding is given up DELIBERATELY and the reason is recorded,
		// not glossed: `trimmedOrNil` (vehicle.go:258) is DR-W7-15 — "a pointer to \"\" and a
		// pointer to spaces all mean the officer said nothing" — so leading/trailing space is
		// ABSENCE at the typing moment, not data. Internal spacing is data and must survive.
		// ⚠ `w7SeedVehicleID` IS DELIBERATELY NOT USED HERE: its second routing guard fires on
		// exactly this input ("a padded value is a ROUTED site"), and §62 bar 2 makes that firing
		// a finding. The record is created directly so the trimming is the SUBJECT rather than an
		// obstacle.
		// ⟦was: "VehicleEntered: \"  TRUCK   12\", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: paddedVehicleID, DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// ⚠⚠ ⟦ROUTED BACK — §62, 2026-09-03⟧ THE ARRIVAL HALF OF THE TRIMMING CASE, ROUTED WITH ITS
	// TRIP HALF ABOVE, on the same finding: it fits none of R1/R2/R3. It compiles today (§48's
	// silent break — the fourth positional parameter's type did not change), so unlike its trip
	// half it is DEAD rather than red, and that is the more dangerous state of the two.
	//
	// ⚠ ⟦ROUTED — §50 arrival re-seed, §47 condition 3 (P2), 2026-09-03⟧ NOT RE-SEEDED — the
	// ARRIVAL half of the TRIMMING case, and it routes for the mirror reason its trip half does.
	// `"Truck 12 "` carries a TRAILING space; `CreateVehicle` applies trimmedOrNil, so the record
	// would hold "Truck 12" and the derived snapshot would lose it. This test asserts
	// `bytes.Contains(dump, "\"Truck 12 \"")` — "the arrival's verbatim entered vehicle id".
	// §50 names the trimming case in terms as one that STAYS ROUTED. PLAN OWNER's.
	// ⟦LOCK — §65, 2026-09-03⟧ B1, the arrival half — §50's same-record transformation.
	// ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, \"site-b\", 1, \"Truck 12 \", \"site-a\", …)"⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, paddedVehicleID, "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if norm := NormalizeVehicleID("  TRUCK   12"); norm != NormalizeVehicleID("Truck 12 ") || norm != "truck 12" {
		t.Fatalf("test precondition broken: entered forms must normalise to %q", "truck 12")
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		// The comparison worked: correlated despite case + whitespace.
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != trip.TripID {
			t.Fatalf("correlation across case/whitespace failed: %+v", d)
		}
		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		// ⟦LOCK — §65, 2026-09-03⟧ THE NEEDLE KEEPS ITS INTERNAL SPACING AND GIVES UP ONLY THE
		// PADDING. ⚠ "TRUCK   12" carries THREE spaces between the words; that run is the whole
		// falsifiability of this check. A needle of "TRUCK 12" would be satisfied by a store that
		// collapsed the run, which is precisely the mangling M8 exists to forbid — see the
		// produced condition in reports/window-build/S62-ROUTES.md §9.
		// ⟦was: "if !bytes.Contains(dump, []byte(`\"  TRUCK   12\"`)) { … }"⟧
		// ⚠⚠ ASSERTED ON THE TRIP'S OWN PROJECTED FIELD, NOT AS A BARE DUMP SUBSTRING, AND THAT
		// CORRECTION WAS FORCED BY A PRODUCED FAILURE OF THE FIRST ATTEMPT. §65 B1's condition
		// was to show the check fails when internal spacing is collapsed. A first version read
		// `bytes.Contains(dump, "TRUCK   12")` — and it PASSED under a plant that mangled the
		// number on the way INTO storage, because THE VEHICLE RECORD'S OWN number is in the same
		// dump and still held the un-collapsed string. ⭐ The needle was being satisfied by a
		// different byte-range than the criterion is about: a check that could not fail for its
		// own reason. See reports/window-build/S62-ROUTES.md §9 for both plants.
		trips, err := Trips(st)
		if err != nil {
			t.Fatal(err)
		}
		var stored *string
		for _, tr := range trips {
			if tr.TripID == trip.TripID {
				stored = tr.VehicleEntered
			}
		}
		if stored == nil || *stored != "TRUCK   12" {
			t.Fatalf("the TRIP's stored vehicle number is %q, want %q — storage must hold what was typed, "+
				"unmangled. Leading/trailing space is dropped at the typing moment by design (trimmedOrNil, "+
				"DR-W7-15: padding is ABSENCE, not data); the three spaces between TRUCK and 12 are DATA and "+
				"must survive into the projection", derefString(stored), "TRUCK   12")
		}
		// ⟦was: "if !bytes.Contains(dump, []byte(`\"Truck 12 \"`)) { t.Fatal(\"arrival's verbatim entered vehicle id is not in storage\") }"⟧
		// ⚠ RETIRED AS A SEPARATE NEEDLE, and the reason is that it can no longer be a DIFFERENT
		// one. Under DR-W7-4 both halves name the same record, so the arrival's snapshot is
		// derived from the same number as the trip's — asserting it a second time would be the
		// same byte-range checked twice, dressed as two checks. What the arrival half still
		// proves is asserted above, by its disposition: it CORRELATED.
		//
		// ⚠ WHAT THIS TEST NO LONGER EXERCISES, said plainly rather than left to be discovered.
		// The old fixture typed "  TRUCK   12" at the trip and "Truck 12 " at the arrival, so it
		// also proved correlation ACROSS CASE AND WHITESPACE. That discrimination cannot be posed
		// here any more: correlation is by record ID, and one record cannot be typed two ways.
		// It is not lost — it moved to where the normaliser itself is tested:
		// `TestW7NormalizeVehicleIDIsUnchanged` (w7_correlation_test.go:523) and
		// `TestW7CorrelationRuleTable` (:241). The precondition below is kept for the same
		// reason: it still states, on the two ORIGINAL typed forms, that the normaliser folds
		// them together.
		if bytes.Contains(dump, []byte("truck 12")) {
			t.Fatal("the normalised form \"truck 12\" appears in the projection dump — normalisation leaked into storage (M8 not-done-if)")
		}
	})
}

// --- done-when: two open trips, same vehicle+origin → ambiguous review ---

// With TWO open candidate trips the fold binds NOTHING: the arrival lands in
// the ambiguous review lane (candidates listed oldest-first by source key),
// both trips stay open, and the arrival's visits still stand. Auto-picking
// "the most plausible" trip here would mark a vehicle arrived that has not
// arrived — the exact failure this design exists to prevent (§9.2).
func TestTwoOpenTripsSameVehicleOriginIsAmbiguousReviewAndNeverAutoPicks(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(60)
	trip1, err := Dispatch(w.a, "site-a", 1, DispatchInput{
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
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip2, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED — same cause as above:
		// the arrival half of this fixture passes a NUMBER where `W7-4(d)(i)` now wants a
		// vehicle RECORD id, so re-seeding the trip half makes the assertion FALSE.
		// ⟦was: "VehicleEntered: "truck  12", DriverPersonID: w.passenger.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "truck  12"), DriverPersonID: w.passenger.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "TRUCK 12", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "TRUCK 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalAmbiguous {
			t.Fatalf("disposition = %s, want ambiguous — the fold auto-picked between two open trips", d.Disposition)
		}
		if d.TripID != nil {
			t.Fatalf("ambiguous arrival is bound to trip %s — an ambiguous match must bind NOTHING", *d.TripID)
		}
		wantCandidates := trip1.TripID + "," + trip2.TripID // oldest first by (node_epoch, seq)
		if d.Candidates == nil || *d.Candidates != wantCandidates {
			t.Fatalf("candidates = %v, want %q in source-key order", d.Candidates, wantCandidates)
		}
		for _, tripID := range []string{trip1.TripID, trip2.TripID} {
			if s := stateOf(t, st, tripID); s.State != TripStateOpen {
				t.Fatalf("trip %s state = %s, want still open — an ambiguous arrival must not close an expectation", tripID, s.State)
			}
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.Ambiguous) != 1 || lane.Ambiguous[0].ArrivalID != arr.ArrivalID {
			t.Fatalf("review lane does not surface the ambiguous arrival: %+v", lane.Ambiguous)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 1 {
			t.Fatalf("ambiguous arrival has %d open visit(s), want 1 — the person is on site whatever the correlation", len(got))
		}
	})
}

// --- done-when (§9.2): unsolicited arrival with no matching trip stays a plain visit ---

func TestUnsolicitedArrivalWithNoMatchingTripRemainsPlainVisit(t *testing.T) {
	w := newC6World(t)
	w.syncBFromA() // roster only; no trips exist anywhere
	// ⭐ ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ RE-ARMED. RESEED-164.md §3.9 named this test as
	// one of TWO vacuous greens that pass had no site in: "no matching trip -> plain" was
	// unfalsifiable, because the arrival carried a NUMBER where `W7-4(d)(i)` wants a vehicle
	// RECORD id and so could match nothing whatever the store held. It now carries an id.
	// PRODUCED [plant P3 — this fixture with the presence of a matching trip as the only variable]:
	//     no trip at all (this fixture) -> plain
	//     one matching open trip        -> correlated
	// See reports/window-build/ARRIVAL-RESEED.md.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "VAN 9", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "VAN 9"), "site-a",
		[]string{w.passenger.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalPlain || d.TripID != nil {
			t.Fatalf("disposition = %+v, want plain and unbound", d)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 1 {
			t.Fatalf("plain arrival has %d open visit(s), want 1", len(got))
		}
	})
}

// --- row 12: the degenerate local trip is the SAME machinery (§10) ---

// A no-destination/no-deadline trip plus a same-site arrival is the ordinary
// gate check-in: the same dispatch fold checks people out, the same arrival
// fold creates snapshot visits, the same state fold closes the expectation —
// and the trip never appears on any inbound board (no destination), which is
// also what C7's replay gate relies on ("degenerate trips untouched").
// Downstream: C8's check-in form authors exactly this pair; C7's scanner
// selects on DeadlineMinutes != nil.
func TestDegenerateLocalTripRunsTheSameMachineryAndSkipsTheBoard(t *testing.T) {
	w := newC6World(t)
	trip, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRK-14"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		// no destination, no deadline — the §10 degenerate trip
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if trip.DestinationSite != nil || trip.DeadlineMinutes != nil {
		t.Fatal("degenerate trip must carry neither destination nor deadline")
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCompany(w.b, "site-b", 1, "Unrelated Co", 0, "default"); err != nil {
		t.Fatal(err) // second stream for permutation
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		if s := stateOf(t, st, trip.TripID); s.State != TripStateArrived {
			t.Fatalf("degenerate trip state = %s, want arrived via the same fold", s.State)
		}
		visits := openVisitsAt(t, st, "site-a")
		if len(visits) != 2 {
			t.Fatalf("local check-in created %d open visit(s), want 2 — same machinery, per-occupant visits", len(visits))
		}
		for _, v := range visits {
			if v.CompanySnapshotID == nil {
				t.Fatalf("visit %s has no point-in-time snapshot — the degenerate path must not skip 8.8-i", v.VisitID)
			}
		}
		for _, site := range []string{"site-a", "site-b"} {
			exps, err := OpenExpectations(st, site)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range exps {
				if e.Trip.TripID == trip.TripID {
					t.Fatalf("degenerate trip appears on %s's inbound board — no destination means no expectation anywhere", site)
				}
			}
		}
	})
}

// --- authoring-time refusals (never author on refusal) ---

func TestTripAuthoringRefusals(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §62, 2026-09-03⟧ R3 — THE HOIST, for the SECOND of the two refusals below only.
	// It must stay ABOVE the `before` read: that is the whole of §62's R3 remedy, and moving it
	// one line down re-creates the defect the route describes.
	// ⚠ ONLY "T 1" IS SEEDED. The first refusal's value is whitespace-only and `CreateVehicle`
	// refuses it outright — seeding it would fire `w7SeedVehicleID`'s routing guard, which §62
	// bar 2 calls "a finding, not an obstacle". It is not seeded; see its own note below.
	driverlessVehicleID := w7SeedVehicleID(t, w.a, "site-a", "T 1")
	before, err := w.a.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62⟧ NEITHER OF THE NEXT TWO
	// IS RE-SEEDED. Both sit between `before` and `after` above/below, which pin "a refusal
	// authored an event". Seeding a vehicle record here AUTHORS ONE, so the transformation would
	// redden the watermark assertion. The first also carries a whitespace-only number
	// CreateVehicle refuses outright. PLAN OWNER's — see reports/window-build/RESEED-164.md §3.
	//
	// ⚠⚠ THE ROUTE IS RIGHT AND THE TWO ARE DIFFERENT CLASSES — its own last sentence is why.
	// They are handled separately below and neither disposition was applied to the other.
	//
	// ⟦LOCK — §62, 2026-09-03⟧ FIRST — "   ", the WHITESPACE refusal. ⚠ NOT class R1, though §62's R1 list names the
	// `"  "` arms. [MEASURED — `tripauthor.go:223-225`] `if vehicleID == "" { … "vehicle id is
	// required" }` is STILL AT DISPATCH, under a comment reading "What moves to vehicle creation
	// is the PLACEHOLDER refusal; the names-an-entity refusal STAYS." §39.4(ii) ruled this arm
	// explicitly: "NOT struck. It is RE-POINTED at the `vehicle_id`, and THE LITERAL IS PRESERVED
	// BYTE-FOR-BYTE." So the field is re-pointed and nothing else moves — same refusal, same
	// layer, same window. ⚠ It also CANNOT be seeded: `CreateVehicle` refuses a whitespace-only
	// number, so this site is barred from the transformation twice over.
	// ⟦was: "VehicleEntered: \"   \", DriverPersonID: w.driver.PersonID,"⟧
	if _, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{VehicleID: "   ", DriverPersonID: w.driver.PersonID}, 0, "default"); err == nil {
		t.Fatal("whitespace-only vehicle id accepted")
	}
	// ⟦LOCK — §62, 2026-09-03⟧ SECOND — "T 1", the DRIVERLESS refusal, and it is class R3. Its subject is the DRIVER rule,
	// not the vehicle: the vehicle value is a fixture detail that only has to be lawful, and the
	// refusal fires before the vehicle record is ever read. So nothing here became false and
	// nothing became unconstructible — the only obstacle was the event a seed authors inside the
	// window, which is R3's mechanism and is answered by the hoist above.
	// ⟦was: "VehicleEntered: \"T 1\","⟧
	if _, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{VehicleID: driverlessVehicleID}, 0, "default"); err == nil {
		t.Fatal("driverless dispatch accepted")
	}
	if err := CancelTrip(w.b, "site-b", 1, trip.TripID, nil, 0, "default"); err == nil || !strings.Contains(err.Error(), "not known") {
		t.Fatalf("cancel from a site that never heard the trip: %v", err)
	}
	w.syncBFromA()
	if err := CancelTrip(w.b, "site-b", 1, trip.TripID, nil, 0, "default"); err == nil || !strings.Contains(err.Error(), "origin-site-authored") {
		t.Fatalf("cancel is origin-site-authored only (§9.1), got: %v", err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, "site-a-trip-doesnotexist", nil, 0, "default"); err == nil {
		t.Fatal("expected arrival for an unheard trip accepted — must refuse toward the unsolicited path")
	}
	// ⚠ ⟦ROUTED — §50 arrival re-seed, §47 condition 3 (P1/P4+P5), 2026-09-03⟧ NEITHER OF THE NEXT
	// TWO IS RE-SEEDED, and they sit in the WATERMARK window §50 names as staying routed.
	// P5 covers both: each is asserted to REFUSE, so neither authors an arrival and a seed restores
	// no bind — it only adds an event inside a block whose whole subject is "a refusal authored
	// nothing". P1 also fails the first outright: `CreateVehicle` refuses a whitespace-only number.
	// ⚠ MEASURED, so the route is not stronger than the fact: `before`/`after` read
	// `w.a.Watermark("site-a", 1)` while these two author on w.b/site-b, so a seed would not in
	// fact move THAT counter. The route stands on P5 and on the block's stated subject, not on a
	// watermark claim that does not hold. PLAN OWNER's, with the two dispatch arms above.
	if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "  ", "site-a", []string{w.driver.PersonID}, nil, 0, "default"); err == nil {
		t.Fatal("unsolicited arrival with blank vehicle accepted — it is the correlation key")
	}
	if _, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "VAN 9", "site-a", nil, nil, 0, "default"); err == nil {
		t.Fatal("unsolicited arrival with no occupants accepted")
	}

	after, err := w.a.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("a refusal authored an event: watermark %d -> %d", before, after)
	}

	// Arrived trips refuse both a second expected arrival and a late cancel.
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default"); err == nil {
		t.Fatal("second expected arrival for an arrived trip accepted")
	}
	for _, ev := range w.events(w.b, "site-b") {
		if _, err := w.a.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := CancelTrip(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err == nil {
		t.Fatal("cancel of a locally-known-arrived trip accepted")
	}
}

// --- review fix (C6 review, Ruling 2): unsolicited arrival vs cancelled trip ---

// {trip, cancel, unsolicited arrival} IS the severed-link race — the
// unsolicited author can, by construction, never have heard the cancel. The
// §9.1 contradiction ("both a cancel and an arrival exist") must reach a
// human REVIEW disposition on the arrival, while the fold still binds
// NOTHING: the trip stays cancelled, no alert logic changes, the visits
// stand. Set-based recompute makes this one test cover both narratives —
// cancel-before-arrival AND a previously-correlated arrival retroactively
// dissolved by a partition-authored cancel syncing in — because they are the
// same event set, and every permuted order here must agree.
func TestUnsolicitedArrivalForCancelledTripLandsInReviewNeverSilentlyPlain(t *testing.T) {
	w := newC6World(t)
	dest := "site-b"
	deadline := int64(60)
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
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelTrip(w.a, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.syncBFromA() // roster only matters; the arrival is unsolicited regardless
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "truck 12", "site-a","⟧
	arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "truck 12"), "site-a",
		[]string{w.driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
		d := dispositionOf(t, st, arr.ArrivalID)
		if d.Disposition != ArrivalCancelledMatch {
			t.Fatalf("disposition = %s, want %s — the cancelled-trip contradiction was silently swallowed (§9.1)", d.Disposition, ArrivalCancelledMatch)
		}
		if d.TripID != nil {
			t.Fatalf("arrival bound to %s — a cancelled-trip match must bind NOTHING", *d.TripID)
		}
		if d.Candidates == nil || *d.Candidates != trip.TripID {
			t.Fatalf("candidates = %v, want the cancelled trip %s so the reviewer can see the contradiction", d.Candidates, trip.TripID)
		}
		if s := stateOf(t, st, trip.TripID); s.State != TripStateCancelled {
			t.Fatalf("trip state = %s, want still cancelled — review is surfaced on the arrival, the fold resolves nothing", s.State)
		}
		lane, err := LoadReviewLane(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(lane.CancelledMatches) != 1 || lane.CancelledMatches[0].ArrivalID != arr.ArrivalID {
			t.Fatalf("review lane does not surface the cancelled-trip match: %+v", lane)
		}
		if got := openVisitsAt(t, st, "site-b"); len(got) != 1 {
			t.Fatalf("arrival has %d open visit(s), want 1 — the person is on site whatever the correlation", len(got))
		}
	})
}

// --- review fix (C6 review, Fix 2): degenerate trips are never candidates ---

// A degenerate local trip left open (the check-in is two authored events; a
// crash between them leaves the trip open forever) appears on NO board — so
// if it could match unsolicited candidacy it would invisibly poison every
// future (vehicle, origin) correlation. Destination-NULL trips are therefore
// excluded from candidacy outright: alone they must NOT be bound, and next
// to a real open trip they must NOT manufacture ambiguity.
func TestOpenDegenerateTripIsNeverACorrelationCandidate(t *testing.T) {
	// Scenario A: ONLY the stale degenerate trip matches → plain, unbound.
	t.Run("alone: plain visit, never bound", func(t *testing.T) {
		w := newC6World(t)
		// The crash window: degenerate trip authored, its arrival never was.
		if _, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
			// ⚠⚠ ⟦DISARMED — §47 re-seed, 2026-09-03⟧ SCENARIO A IS RE-SEEDED AND WILL NOW PASS
			// VACUOUSLY; SCENARIO B BELOW IS ROUTED FOR THE MIRROR REASON. The unsolicited call four
			// lines down passes a NUMBER where `W7-4(d)(i)` now wants a vehicle RECORD id — same
			// arity, so it compiles — and a number names no record, so the arrival correlates with
			// NOTHING. A asserts `ArrivalPlain`, now unfalsifiable; B asserts `ArrivalCorrelated`,
			// now FALSE. ⚠ A's green is NOT evidence that a degenerate trip is excluded from
			// candidacy — nothing is a candidate any more.
			// See reports/window-build/RESEED-164.md §3.9.
			// ⭐ ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ RE-ARMED. The unsolicited call below now
			// passes a vehicle RECORD id, so A's `ArrivalPlain` is falsifiable again. PRODUCED
			// [plant P2 — the same fixture with the trip's DEGENERACY as the only variable]:
			//     degenerate trip (this fixture) -> plain
			//     real destination (the mutation) -> correlated
			// ⚠ Scenario B's route is DISCHARGED in the same pass, so the two arms are once more
			// the pair they were written as. See reports/window-build/ARRIVAL-RESEED.md.
			// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.driver.PersonID,"⟧
			VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.driver.PersonID,
		}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		w.syncBFromA()
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "truck 12", "site-a","⟧
		arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "truck 12"), "site-a",
			[]string{w.driver.PersonID}, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
			if d := dispositionOf(t, st, arr.ArrivalID); d.Disposition != ArrivalPlain || d.TripID != nil {
				t.Fatalf("arrival with only a degenerate trip in range: %+v, want plain and unbound — a local check-in is not an inbound expectation", d)
			}
		})
	})

	// Scenario B: a stale degenerate AND a real open trip both match the key
	// → the arrival binds the real trip CLEANLY; the degenerate must not
	// manufacture an ambiguity that sends every arrival for this vehicle to
	// review forever.
	t.Run("beside a real trip: correlates, no manufactured ambiguity", func(t *testing.T) {
		w := newC6World(t)
		if _, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
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
		}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		dest := "site-b"
		deadline := int64(60)
		real, err := Dispatch(w.a, "site-a", 1, DispatchInput{
			// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
			// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
			// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
			// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
			// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
			// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
			// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED — same cause as above:
			// the arrival half of this fixture passes a NUMBER where `W7-4(d)(i)` now wants a
			// vehicle RECORD id, so re-seeding the trip half makes the assertion FALSE.
			// ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: w.passenger.PersonID,"⟧
			VehicleID: w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"), DriverPersonID: w.passenger.PersonID,
			DestinationSite: &dest, DeadlineMinutes: &deadline,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		w.syncBFromA()
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "TRUCK 12", "site-a","⟧
		arr, err := RecordUnsolicitedArrival(w.b, "site-b", 1, w7SeedVehicleID(t, w.b, "site-b", "TRUCK 12"), "site-a",
			[]string{w.passenger.PersonID}, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		replayAllOrders(t, w.events(w.a, "site-a"), w.events(w.b, "site-b"), func(t *testing.T, st *store.Store) {
			d := dispositionOf(t, st, arr.ArrivalID)
			if d.Disposition != ArrivalCorrelated || d.TripID == nil || *d.TripID != real.TripID {
				t.Fatalf("arrival beside a real open trip: %+v, want correlated to %s — the stale degenerate trip must not manufacture ambiguity", d, real.TripID)
			}
		})
	})
}
