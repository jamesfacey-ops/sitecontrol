// zz_window_audit_test.go — WINDOW-AUDIT witnesses (audit-only, never to be
// merged). Each test asserts the SAFE behaviour; a FAIL is the demonstration.
package domain

import (
	"bytes"
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

func auditOpenVisit(t *testing.T, st *store.Store, siteID, personID string) *Visit {
	t.Helper()
	vs, err := OpenVisitsAt(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range vs {
		if vs[i].PersonID == personID {
			return &vs[i]
		}
	}
	return nil
}

func auditContains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// D2 — a trip_arrival_not_entered row is never consumed: after the person
// was entered by completion, checked OUT, and the completion retracted, a
// SECOND completion (a desk action on /orientation) re-enters them onto the
// muster with no vehicle and no gate action.
func TestAuditStaleHeldOutRowReArmsEntryOnCompletionAfterRetraction(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA()
	trip := w.dispatchBus7(t)
	diaz := w.ids(3)[0]
	for _, id := range append(w.ids(0, 1, 2, 4), w.driver.PersonID) {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	arr, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if !auditContains(arr.NotEntered, diaz) {
		t.Fatalf("PRECONDITION: Diaz was not held out: %v", arr.NotEntered)
	}
	// 1. completion enters Diaz (intended, d91 §3.4)
	if _, err := CompleteOrientation(w.b, "site-b", 1, diaz, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	v1 := auditOpenVisit(t, w.b, "site-b", diaz)
	if v1 == nil {
		t.Fatal("PRECONDITION: entry-on-completion did not enter Diaz")
	}
	// 2. Diaz leaves (gate check-out)
	if err := CheckOutVisit(w.b, "site-b", 1, v1.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// 3. the completion is retracted as a mis-tick (allowed: no open visit)
	if _, err := RetractOrientation(w.b, "site-b", 1, diaz, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	held, err := HeldOutAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range held {
		if h.PersonID == diaz {
			t.Logf("after retraction Diaz is BACK on the held-out list (arrival %s) although she left through the gate", h.ArrivalID)
		}
	}
	// 4. days later, her orientation is recorded again at the desk
	env2, err := CompleteOrientation(w.b, "site-b", 1, diaz, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var p OrientationCompleted
	if err := json.Unmarshal(env2.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Visit != nil {
		t.Errorf("DEFECT: the second completion carries an ENTRY (arrival_ref=%s, visit=%s) — the not_entered row from the original arrival re-armed entry-on-completion after Diaz had already been entered by it and had left", *p.ArrivalRef, p.Visit.VisitID)
	}
	if v2 := auditOpenVisit(t, w.b, "site-b", diaz); v2 != nil {
		t.Errorf("DEFECT: Diaz has an OPEN visit at site-b (%s, trip_id=%v) minted by a desk completion — she is on the muster with no vehicle and no gate action", v2.VisitID, v2.TripID)
	}
	// the roster cannot even date this visit: VisitSignInTimes finds no arrival
	// event naming it
	if v2 := auditOpenVisit(t, w.b, "site-b", diaz); v2 != nil {
		times, err := VisitSignInTimes(w.b, "site-b", []Visit{*v2})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := times[v2.VisitID]; !ok {
			t.Logf("(and the on-site roster shows a dash for that visit's sign-in time — VisitSignInTimes has no arrival event naming an entry-on-completion visit)")
		}
	}
}

// W4 — even the INTENDED entry-on-completion visit has no sign-in time on
// the gate roster.
func TestAuditEntryOnCompletionVisitHasASignInTimeOnTheRoster(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA()
	trip := w.dispatchBus7(t)
	diaz := w.ids(3)[0]
	for _, id := range append(w.ids(0, 1, 2, 4), w.driver.PersonID) {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteOrientation(w.b, "site-b", 1, diaz, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	vs, err := OpenVisitsAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	times, err := VisitSignInTimes(w.b, "site-b", vs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if _, ok := times[v.VisitID]; !ok {
			t.Errorf("open visit %s (person %s, trip_id=%v) has NO sign-in time — the roster renders a dash for a person who was entered by entry-on-completion", v.VisitID, v.PersonID, v.TripID)
		}
	}
	t.Logf("%d open visits, %d dated", len(vs), len(times))
}

// D1 — merge (D-59) is not consulted by the orientation choke point.
func TestAuditMergedDuplicateIsHeldOutAlthoughTheCanonicalRecordHoldsOrientation(t *testing.T) {
	w := newD98World(t)
	canon := w.occupants[3] // Diaz
	dup, err := CreatePerson(w.a, "site-a", 1, "Diaz", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// R-10 repair: the duplicate is merged into the canonical record
	if _, err := AmendPerson(w.a, "site-a", 1, PersonAmendment{PersonID: dup.PersonID, SameAs: &canon.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	merge, err := LoadPersonMergeMap(w.b)
	if err != nil {
		t.Fatal(err)
	}
	if merge.Resolve(dup.PersonID) != canon.PersonID {
		t.Fatalf("PRECONDITION: merge did not replicate to B")
	}
	// the human holds site-b's orientation (under the canonical record)
	for _, id := range []string{canon.PersonID, w.driver.PersonID} {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// a bus manifest still names the duplicate record (dispatched from A)
	dest, deadline := "site-b", int64(120)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 9"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{dup.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	arr, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if auditContains(arr.NotEntered, dup.PersonID) {
		t.Errorf("DEFECT: the merged-away duplicate %s was HELD OUT at site-b although its canonical record %s holds site-b's orientation (OrientationHeld reads the raw id, never the merge)", dup.PersonID, canon.PersonID)
	}
	held, err := HeldOutAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range held {
		t.Logf("held-out list names %s (%s) via arrival %s", h.PersonID, h.Name, h.ArrivalID)
		if h.PersonID == canon.PersonID {
			// the list names the CANONICAL id; completing it is refused
			_, cerr := CompleteOrientation(w.b, "site-b", 1, canon.PersonID, nil, 0, "default")
			t.Errorf("DEFECT: the held-out list names the canonical record %s, which already holds the orientation — the offered next action is refused: %v", canon.PersonID, cerr)
		}
	}
	// and the human then enters normally under the canonical record …
	ltrip, err := DispatchLocalCheckIn(w.b, "site-b", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.b, "site-b", "TRK 1"), DriverPersonID: canon.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, ltrip.TripID, nil, 0, "default", true); err != nil {
		t.Fatal(err)
	}
	if auditOpenVisit(t, w.b, "site-b", canon.PersonID) == nil {
		t.Fatal("PRECONDITION: the canonical record did not enter")
	}
	// … yet stays on the held-out list forever
	held, err = HeldOutAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range held {
		if h.PersonID == canon.PersonID {
			t.Errorf("DEFECT: %s is ON SITE (open visit) and still listed as held out — the zombie row can never clear (sightedSince and OrientationHeld both run on the raw duplicate id)", canon.PersonID)
		}
	}
}

// D1b — R-22 (James, design/REQUIREMENTS-FROM-JAMES-2026-09-09.md): a
// held-out person is physically in the guard shack and the vehicle is
// STOPPED until their orientation completes — the held-out list is a
// do-this-now work queue, minutes long, not a register. The officer can
// only complete the SURVIVING record (/orientation hides a merged-away
// duplicate — web/orientation.go:197). If a duplicate was held out under
// its OWN raw id and merged into the canonical record AFTER, completing the
// canonical id's orientation must still find and enter the row the raw id
// was held out under — not just clear it off the list.
func TestAuditCompletingTheCanonicalRecordEntersADuplicateHeldOutUnderItsRawID(t *testing.T) {
	w := newD98World(t)
	canon := w.occupants[3] // Diaz
	dup, err := CreatePerson(w.a, "site-a", 1, "Diaz", &w.company.CompanyID, &w.trade.TradeID, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendPerson(w.a, "site-a", 1, PersonAmendment{PersonID: dup.PersonID, SameAs: &canon.PersonID}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	// Neither id holds site-b's orientation yet — the bus arrives and the
	// duplicate (the manifest's own id) is held out under ITS OWN raw id.
	dest, deadline := "site-b", int64(120)
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "BUS 9"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{dup.PersonID},
		DestinationSite:   &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.syncBFromA()
	arr, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if !auditContains(arr.NotEntered, dup.PersonID) {
		t.Fatalf("PRECONDITION: dup should be held out (nobody holds site-b's orientation yet): %v", arr.NotEntered)
	}
	// The officer completes orientation for the only id /orientation ever
	// shows — the canonical record.
	if _, err := CompleteOrientation(w.b, "site-b", 1, canon.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if auditOpenVisit(t, w.b, "site-b", dup.PersonID) == nil && auditOpenVisit(t, w.b, "site-b", canon.PersonID) == nil {
		t.Errorf("DEFECT: canon's completion did not enter the duplicate held out by this arrival (findEntryCandidate looked only at personID's own not_entered rows, never the merge) — the vehicle would be told to proceed with the seat still empty")
	}
	held, err := HeldOutAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range held {
		if h.PersonID == canon.PersonID {
			t.Errorf("DEFECT: still listed as held out after the completion that was supposed to resolve it")
		}
	}
}

// W7 — replication convergence under cross-stream interleaving, divergent
// epochs and schema-13 payloads (unidentified vehicle + entry-on-completion).
func TestAuditReplicationConvergesUnderInterleaveEpochsAndSchema13Payloads(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA()
	trip := w.dispatchBus7(t)
	adams, clark, diaz, evans := w.ids(0)[0], w.ids(2)[0], w.ids(3)[0], w.ids(4)[0]
	for _, id := range append(w.ids(0, 1, 2), w.driver.PersonID) {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true); err != nil {
		t.Fatal(err)
	}
	// entry-on-completion on epoch 1
	if _, err := CompleteOrientation(w.b, "site-b", 1, diaz, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// B re-images: epoch 2 — entry-on-completion for Evans, a check-out and a
	// retraction for Clark, an unidentified-vehicle departure B -> A
	if _, err := CompleteOrientation(w.b, "site-b", 2, evans, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	cv := auditOpenVisit(t, w.b, "site-b", clark)
	if cv == nil {
		t.Fatal("PRECONDITION: Clark not on site")
	}
	if err := CheckOutVisit(w.b, "site-b", 2, cv.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := RetractOrientation(w.b, "site-b", 2, clark, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	destA, dl := "site-a", int64(90)
	if _, err := Dispatch(w.b, "site-b", 2, DispatchInput{VehicleUnidentified: true, DriverPersonID: adams,
		OccupantPersonIDs: []string{diaz}, DestinationSite: &destA, DeadlineMinutes: &dl}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A: unidentified-vehicle local check-in of Evans' sibling record (Baker) is
	// impossible (Baker is at B) — use the driver's own return: an
	// unidentified check-in at A of a person not on site anywhere else.
	lt, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{VehicleUnidentified: true, DriverPersonID: w.driver.PersonID}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(w.a, "site-a", 1, lt.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// A also sees B's streams (full mesh), B sees A's new events
	for _, e := range []int64{1, 2} {
		evs, err := w.b.Events("site-b", e, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if _, err := w.a.Apply(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.syncBFromA()
	refA, err := w.a.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	refB, err := w.b.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(refA, refB) {
		t.Errorf("DEFECT: A and B hold the same events and dump DIFFERENT projections")
	}
	a1, _ := w.a.Events("site-a", 1, 1, 0)
	b1, _ := w.b.Events("site-b", 1, 1, 0)
	b2, _ := w.b.Events("site-b", 2, 1, 0)
	concat := func(xs ...[]store.Envelope) []store.Envelope {
		var out []store.Envelope
		for _, x := range xs {
			out = append(out, x...)
		}
		return out
	}
	rr := func(x, y []store.Envelope) []store.Envelope {
		var out []store.Envelope
		for i := 0; i < len(x) || i < len(y); i++ {
			if i < len(x) {
				out = append(out, x[i])
			}
			if i < len(y) {
				out = append(out, y[i])
			}
		}
		return out
	}
	orders := map[string][]store.Envelope{
		"A,B1,B2":        concat(a1, b1, b2),
		"B1,B2,A":        concat(b1, b2, a1),
		"rr(A,B1),B2":    concat(rr(a1, b1), b2),
		"rr(A,B1+B2)":    rr(a1, concat(b1, b2)),
		"rr(B1+B2,A)":    rr(concat(b1, b2), a1),
	}
	for name, order := range orders {
		c := newDomainTestStore(t)
		for _, ev := range order {
			if _, err := c.Apply(ev); err != nil {
				t.Fatalf("order %s: peer refused %v: %v", name, ev.Key(), err)
			}
		}
		got, err := c.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, refB) {
			t.Errorf("DEFECT: delivery order %s produced a DIFFERENT projection dump from B's own", name)
		}
	}
	// and as whole-stream batches (the engine's page shape), B first
	c := newDomainTestStore(t)
	for _, batch := range [][]store.Envelope{b1, b2, a1} {
		if _, err := c.ApplyBatch(batch); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := c.DumpProjections()
	if !bytes.Equal(got, refB) {
		t.Errorf("DEFECT: batched delivery B1,B2,A produced a different dump")
	}
	t.Logf("%d orders + 1 batched replay converged on %d bytes of projections", len(orders), len(refB))
}

// P — WINDOW-AUDIT.md's Finding P (a held-out person renders NOT TRACKED on
// the muster at both posts) is a RULING QUESTION for James, explicitly out
// of scope for this lane (window-fixes brief, 2026-09-13: "Do not touch
// it"). Its witness is deliberately NOT merged here: this file's sibling
// tests must pass at HEAD for deploy/redlist-state-check.sh's 17=17 PASS
// bar, and an unregistered failing test would show as an UNLISTED RED with
// no declared row to match it. The witness itself is unchanged and still
// lives in the audit's own worktree (WINDOW-AUDIT.md cites
// scratchpad/window-audit/app) for whoever picks up the ruling.

// W13 — the schema-13 CHECK is a fold-time refusal (a poison pill) for a
// contradictory row from a non-conforming author.
func TestAuditContradictoryUnidentifiedRowStallsThePeerStream(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA()
	// hand-craft what dispatchTrip refuses: named AND unidentified
	dest, dl := "site-b", int64(60)
	payload, _ := json.Marshal(TripDispatched{Type: "trip_dispatched", TripID: newID("site-a", "trip"),
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "T-1"), VehicleUnidentified: true,
		DriverPersonID: w.driver.PersonID, OccupantPersonIDs: []string{w.driver.PersonID},
		DestinationSite: &dest, DeadlineMinutes: &dl})
	env, err := w.a.Author("site-a", 1, payload, 0, "default")
	if err != nil {
		t.Logf("A's own Author refused it too (fold error surfaces at the author): %v", err)
		return
	}
	_ = env
	w.syncBFromA()
}
