// w9b_seams_test.go — W9b (item 16, F4's un-reference `arrival_ref_retracted`):
// shared fixture machinery for the domain-side W9b criteria. NO ASSERTIONS
// LIVE HERE.
//
// ⚠ THE ONE DESIGN DECISION IN THIS FILE, stated first because every other
// W9b test rests on it.
//
// W9b is greenfield in ALL of its own artefacts. On the pre-chunk tree
// `grep -ra 'arrival_ref_retract\|ArrivalRefRetract\|unref\|Unref'
// --include=*.go app/` returns ZERO production hits [MEASURED, this pass].
// A test that NAMES `domain.ArrivalRefRetracted`, `ReviewLane.RefRetracted`
// or an `arrivalRefRetractionProjection` does not fail — it fails to
// COMPILE, one compile error in `package domain` buries every other W9b red
// behind it, and it breaks every other agent's `go test ./internal/domain`
// while W7/W8 are live. W5a met this fork and recorded the answer
// (`W5a-tests.md` §1.1); W6 chose runtime reds (`W6-tests.md` §1.1); W9a
// chose runtime reds (`w9a_seams_test.go`). W9b follows them.
//
// So NOTHING in W9b's tests names an unbuilt Go symbol. Instead:
//
//   - retractions are authored as HAND-BUILT ENVELOPES carrying the frozen
//     wire JSON of F4 §3.6, applied through the real `store.Apply` — which
//     is deliberately permissive about site_id in tests. That is the WIRE
//     path the criteria name, and it freezes the payload's three literals
//     by construction;
//   - state is observed at readers that exist TODAY (`TripStateByID`,
//     `ArrivalStates`, `OpenExpectations`, `DumpProjection`), never at an
//     `EffectiveArrival`-shaped accessor W9b does not build;
//   - structure goes through `internal/store`'s own package
//     (`w9b_sidetable_structural_test.go`), the technique
//     `w5d_sidetable_structural_test.go` and `w9a_sidetable_structural_test.go`
//     established.
//
// Consequence, wanted: every W9b test COMPILES AND RUNS against the
// pre-chunk tree, so its pre-chunk failure is a produced RUNTIME red naming
// its criterion, never a compile error. The builder's Go names and
// signatures stay the builder's.
//
// ⚠ INDEPENDENT OF W9a ON PURPOSE. `package domain` already carries W9a's
// `w9a*` fixture helpers. W9b defines its own rather than borrowing them:
// W9b's dependency line states "**W9b → W9a: NONE for the build**", and a
// W9b red caused by a change to a W9a helper would be exactly the
// cross-sub-chunk interference W9a's own structural file refused to create
// in the other direction.
//
// --- SEAMS BOUND HERE, ONCE ------------------------------------------------
//
// SEAM W9B-S-DISP — the retracted arrival's `arrival_state.disposition`
// COLUMN VALUE. F4 §3.6 names the Go-level disposition `ArrivalRefRetracted`
// and nothing anywhere declares the wire/column spelling. The five delivered
// spellings are `expected`, `correlated`, `plain`, `ambiguous`,
// `cancelled_match` [MEASURED — trip.go's disposition block]. Rather than
// invent a sixth literal and red a correct build over spelling, the domain
// assertions are STRUCTURAL: the retracted arrival's disposition must be
// NON-EMPTY, must be NONE OF THE FIVE delivered values, and must be the SAME
// value at both posts. What the officer actually sees is pinned where it can
// be pinned without inventing a literal — W9b-5, on the rendered review-lane
// page. A builder who wants a spelling blessed takes it to the GATE.
//
// SEAM W9B-S-RETRACTOR — "the retracting post". F4 §3.6's payload carries
// `recorded_by` as "post attribution (D-5)", and the delivered convention
// for that value is `Config.PostName()` [MEASURED — web/auth.go:163–166].
// Fixtures therefore author `recorded_by` as the retracting post's
// configured display name, and the rendered assertion derives its expected
// value from the config rather than hardcoding a post name (VF §5's rule).
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the frozen wire spellings, as literals (F4 §3.6) --------------------

const (
	w9bEvtRefRetracted = "arrival_ref_retracted"

	// The payload's THREE keys and no others. W9b-2's filter-independent
	// structural assertion is stated over exactly this set.
	w9bKeyType       = "type"
	w9bKeyArrivalID  = "arrival_id"
	w9bKeyRecordedBy = "recorded_by"

	// The projection (F4 §3.6: STRICT, INSERT-only, PK the envelope's own
	// key, columns (site_id, node_epoch, seq, arrival_id, recorded_by)).
	w9bTblRefRetraction = "arrival_ref_retraction"
)

// w9bPayloadKeys is the payload's key set, in sorted order — the literal
// W9b-2's structural half compares against. Stated as an enumeration and
// not as a count, because a count cannot say WHICH three.
var w9bPayloadKeys = []string{w9bKeyArrivalID, w9bKeyRecordedBy, w9bKeyType}

// w9bDeliveredDispositions is the arrival-disposition vocabulary as
// DELIVERED, read off the constants rather than retyped, so a build that
// renames one cannot leave this list quietly stale (the hand-kept-
// enumeration class). SEAM W9B-S-DISP: the retracted arrival must take a
// value outside this set.
var w9bDeliveredDispositions = []string{
	ArrivalExpected, ArrivalCorrelated, ArrivalPlain, ArrivalAmbiguous, ArrivalCancelledMatch,
}

// w9bBase is this suite's fixed audit "now", kept separate from w3Base and
// w9aBase so a W9b fixture never depends on another chunk's constant.
const w9bBase = int64(1_800_000_000_000)

const w9bMinute = int64(60_000)

func w9bStr(s string) *string { return &s }
func w9bI64(v int64) *int64   { return &v }

// --- stores ---------------------------------------------------------------

func w9bNewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w9bReading(w9bBase)
	return st
}

func w9bReading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// --- the wire path --------------------------------------------------------

// w9bEnvelope builds ONE replicated envelope by hand. It is the only way an
// `arrival_ref_retracted` can exist before an authoring surface does, and it
// is deliberately the WIRE shape: the payload map's keys are F4 §3.6's
// frozen literals, so a builder that spells one of them differently reds
// every fixture that carries it.
func w9bEnvelope(siteID string, epoch, seq, wallMS int64, payload any) store.Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("w9b: marshalling a fixture payload: %v", err))
	}
	return store.Envelope{
		SiteID:        siteID,
		NodeEpoch:     epoch,
		Seq:           seq,
		SchemaVersion: store.SchemaVersion,
		OriginAuth:    siteID,
		AuthoredAt: store.AuthoredAt{
			WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced,
		},
		OccurredAt: wallMS,
		TimeSource: "default",
		Payload:    raw,
	}
}

// w9bRetraction is the arrival_ref_retracted payload builder. F4 §3.6: it
// "names the arrival whose reference is withdrawn and NOTHING else — no
// replacement trip, ever."
func w9bRetraction(arrivalID string, recordedBy *string) map[string]any {
	m := map[string]any{
		w9bKeyType:      w9bEvtRefRetracted,
		w9bKeyArrivalID: arrivalID,
	}
	if recordedBy != nil {
		m[w9bKeyRecordedBy] = *recordedBy
	}
	return m
}

// w9bWireArrival is a hand-built `trip_arrival`. T12b needs TWO arrivals
// both carrying `TripRef = X`, and the authoring path refuses the second
// ("trip %s is already arrived" [MEASURED — tripauthor.go's expected-arrival
// guard]). That guard is an AUTHORING-time guard, not a fold guard, and F4
// §12 T12b states the world is "constructible in the delivered tree with no
// new code, since Pass 1 records the second as `ArrivalExpected`" — so the
// second arrival is authored on the wire, which is how a second post's
// arrival reaches this store in the field anyway. Built from the DELIVERED
// struct so its wire spellings cannot drift away from the fold's.
func w9bWireArrival(arrivalID, tripRef string, visits []ArrivalVisit, recordedBy *string) TripArrival {
	p := TripArrival{
		Type: "trip_arrival", ArrivalID: arrivalID,
		Visits: visits, RecordedBy: recordedBy,
	}
	if tripRef != "" {
		p.TripRef = &tripRef
	}
	return p
}

// w9bApply applies one wire envelope. Folds NEVER refuse (F4 §3.6: "the fold
// is a bare INSERT — no lookup, no validation at apply time"), so a refusal
// here is a FINDING and is reported as one — it is mutant m14b's own shape.
func w9bApply(t *testing.T, st *store.Store, env store.Envelope) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9b: apply %v (%s): %v — the fold REFUSED. F4 §3.6: the fold is a bare INSERT, dangling-tolerant, and validates nothing at apply time. "+
			"A refusal here is mutant m14b (apply-time validation dropping a dangling retraction), not a fixture problem",
			env.Key(), w9bPayloadType(env), err)
	}
	if !applied {
		t.Fatalf("w9b: apply %v: deduped unexpectedly", env.Key())
	}
}

func w9bPayloadType(env store.Envelope) string {
	var m map[string]any
	if err := json.Unmarshal(env.Payload, &m); err != nil {
		return "<unparseable payload>"
	}
	s, _ := m[w9bKeyType].(string)
	return s
}

// w9bBuildNext returns the envelope that WOULD land at the stream's next
// free seq, without applying it. Used where a fixture must hold an envelope
// back and deliver it later (the dangling permutation, the crossing case).
func w9bBuildNext(t *testing.T, st *store.Store, siteID string, epoch, wallMS int64, payload any) store.Envelope {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	return w9bEnvelope(siteID, epoch, wm+1, wallMS, payload)
}

// w9bApplyNext applies at the stream's next free seq and returns the
// envelope, so a caller can replicate the same bytes to a peer.
func w9bApplyNext(t *testing.T, st *store.Store, siteID string, epoch, wallMS int64, payload any) store.Envelope {
	t.Helper()
	env := w9bBuildNext(t, st, siteID, epoch, wallMS, payload)
	w9bApply(t, st, env)
	return env
}

// w9bEventsOf returns every stored envelope of one stream, in seq order —
// the bytes a replication engine would hand a peer.
func w9bEventsOf(t *testing.T, st *store.Store, siteID string, epoch int64) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, epoch, 1, 0)
	if err != nil {
		t.Fatalf("w9b: reading stream (%s, %d): %v", siteID, epoch, err)
	}
	return evs
}

// w9bLogPayloadKeys reads the payload of the event at key straight from the
// replicated LOG and returns its sorted key set. W9b-2's structural half is
// stated over the WIRE, not over a Go struct that does not exist yet.
func w9bLogPayloadKeys(t *testing.T, st *store.Store, k store.Key) []string {
	t.Helper()
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		t.Fatalf("w9b: reading the log at %v: %v", k, err)
	}
	if len(evs) != 1 || evs[0].Key() != k {
		t.Fatalf("w9b FIXTURE DEFECT: the log holds no event at %v", k)
	}
	var m map[string]any
	if err := json.Unmarshal(evs[0].Payload, &m); err != nil {
		t.Fatalf("w9b: payload at %v is not a JSON object: %v", k, err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- seeding entities with CHOSEN ids ------------------------------------

func w9bSeedPerson(t *testing.T, st *store.Store, siteID string, epoch int64, personID, name string) string {
	t.Helper()
	w9bApplyNext(t, st, siteID, epoch, w9bBase, map[string]any{
		"type": "person_created", "person_id": personID, "name": name,
	})
	return personID
}

// w9bSeedPersons gives each person a display NAME equal to its id, so every
// rendered name is a literal the fixture already pins.
func w9bSeedPersons(t *testing.T, st *store.Store, siteID string, epoch int64, ids ...string) {
	t.Helper()
	for _, id := range ids {
		w9bSeedPerson(t, st, siteID, epoch, id, id)
	}
}

// --- fixture worlds -------------------------------------------------------

// w9bDispatchOpen dispatches an ordinary controlled trip. The trip is OPEN
// with a live deadline expectation at the destination.
func w9bDispatchOpen(t *testing.T, st *store.Store, origin, destination string, epoch int64, vehicle, driver string, occupants []string, deadlineMinutes int64) Trip {
	t.Helper()
	dest := destination
	trip, err := Dispatch(st, origin, epoch, DispatchInput{
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
		// ⚠ THIS IS A SHARED FIXTURE HELPER. Its callers — listed in RESEED-164.md §4.5 —
		// include files with no `VehicleEntered` site of their own, so a re-seed here reddens
		// tests whose own assertion diff is EMPTY.
		// ⟦was: "VehicleEntered: vehicle, DriverPersonID: driver,"⟧
		VehicleID: w7SeedVehicleID(t, st, origin, vehicle), DriverPersonID: driver,
		OccupantPersonIDs: occupants,
		DestinationSite:   &dest, DeadlineMinutes: w9bI64(deadlineMinutes),
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: dispatch: %v", err)
	}
	return trip
}

// w9bArriveExpected is THE MIS-CLICK: an expected arrival recorded against
// the wrong open trip, which closes it and silences its expectation.
func w9bArriveExpected(t *testing.T, st *store.Store, siteID string, epoch int64, tripID string) Arrival {
	t.Helper()
	a, err := RecordExpectedArrival(st, siteID, epoch, tripID, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: expected arrival for %s: %v", tripID, err)
	}
	return a
}

// --- observing state ------------------------------------------------------

func w9bTripStateRow(t *testing.T, st *store.Store, tripID string) TripState {
	t.Helper()
	s, err := TripStateByID(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatalf("w9b: trip %s has no trip_state row", tripID)
	}
	return *s
}

func w9bTripState(t *testing.T, st *store.Store, tripID string) string {
	t.Helper()
	return w9bTripStateRow(t, st, tripID).State
}

// w9bBoundArrival is `trip_state.arrival_id` — the quantity F4 §3.6 calls
// `boundTo` and the one the D-58 fillers move. Returned as a string so a
// criterion can pin it as a LITERAL arrival id rather than as
// "expected(recompute)".
func w9bBoundArrival(t *testing.T, st *store.Store, tripID string) string {
	t.Helper()
	row := w9bTripStateRow(t, st, tripID)
	if row.ArrivalID == nil {
		return ""
	}
	return *row.ArrivalID
}

func w9bDisposition(t *testing.T, st *store.Store, arrivalID string) string {
	t.Helper()
	rows, err := ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ArrivalID == arrivalID {
			return r.Disposition
		}
	}
	t.Fatalf("w9b: no arrival_state row for arrival %s — it holds %s", arrivalID, w9bDispositionSummary(rows))
	return ""
}

func w9bDispositionSummary(rows []ArrivalState) string {
	if len(rows) == 0 {
		return "nothing"
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.ArrivalID+"="+r.Disposition)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// w9bIsDeliveredDisposition reports whether d is one of the five
// dispositions the pre-chunk tree already has (SEAM W9B-S-DISP).
func w9bIsDeliveredDisposition(d string) bool {
	for _, x := range w9bDeliveredDispositions {
		if x == d {
			return true
		}
	}
	return false
}

// w9bHasExpectation reports whether siteID currently holds an OPEN deadline
// expectation for tripID. OpenExpectations is the shared input of the
// deadline scanner, the inbound console manifest and the /trips board rows,
// so "the expectation and board row are BACK" is observable here without a
// renderer.
func w9bHasExpectation(t *testing.T, st *store.Store, siteID, tripID string) bool {
	t.Helper()
	es, err := OpenExpectations(st, siteID)
	if err != nil {
		t.Fatalf("w9b: OpenExpectations(%s): %v", siteID, err)
	}
	for _, e := range es {
		if e.Trip.TripID == tripID {
			return true
		}
	}
	return false
}

// --- canonical bytes ------------------------------------------------------

func w9bSection(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	b, err := st.DumpProjection(name)
	if err != nil {
		t.Fatalf("w9b: DumpProjection(%q): %v", name, err)
	}
	return string(b)
}

func w9bAllProjections(t *testing.T, st *store.Store) string {
	t.Helper()
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("w9b: DumpProjections: %v", err)
	}
	return string(b)
}
