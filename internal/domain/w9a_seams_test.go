// w9a_seams_test.go — W9a (item 15, F4's amendment family: `trip_amended` +
// `arrival_amended`): shared fixture machinery for the domain-side W9a
// criteria. NO ASSERTIONS LIVE HERE.
//
// ⚠ THE ONE DESIGN DECISION IN THIS FILE, stated first because every other
// W9a test rests on it.
//
// W9a is greenfield in ALL of its own artefacts. On the pre-chunk tree
// `grep -ra 'trip_amendment\|arrival_amendment\|EffectiveTrip\|
// EffectiveArrival' --include=*.go app/` returns ZERO production hits
// [MEASURED]. A test that NAMES `EffectiveTrip`, `domain.TripAmended` or
// `AmendTrip` does not fail — it fails to COMPILE, one compile error in
// `package domain` buries every other W9a red behind it, and it breaks
// every other agent's `go test ./internal/domain` while W7/W8 are live.
// W5a met this fork and recorded the answer (`W5a-tests.md` §1.1); W6 met
// it again and chose runtime reds (`W6-tests.md` §1.1). W9a follows W6.
//
// So NOTHING in W9a's tests names an unbuilt Go symbol. Instead:
//
//   - amendments are authored as HAND-BUILT ENVELOPES carrying the frozen
//     wire JSON of F4 §3.1, applied through the real `store.Apply` — which
//     is deliberately permissive about site_id ("injecting foreign-site
//     events in tests is exactly what makes C5/C6 buildable before
//     replication exists", store/log.go). That is the WIRE PATH the
//     criteria name, and it freezes gate B-2's literals by construction;
//   - the EFFECTIVE record is observed at the §9.2 CONSUMERS that already
//     exist (OpenExpectations, NotTracked, OutAtUncontrolled, InTransitFrom,
//     TripOccupantsByTrip, LoadReviewLane, the rendered pages), never at
//     `EffectiveTrip` — which is what W9a-2 requires anyway: "Neither source
//     scan substitutes for per-consumer observation";
//   - structure and the divergent-writer path go through a second
//     `sql.Open("sqlite", st.Path)` handle, the technique
//     `w5d_sidetable_structural_test.go` established;
//   - raw-row byte-identity goes through `st.DumpProjection` ([[D-56]]: never
//     through an HTTP dump).
//
// Consequence, wanted: every W9a test COMPILES AND RUNS against the
// pre-chunk tree, so its pre-chunk failure is a produced RUNTIME red naming
// its criterion, never a compile error. The builder's Go names and
// signatures stay the builder's.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the frozen wire spellings, as literals (F4 §3.1; gate B-2's list) ---

const (
	w9aEvtTripAmended    = "trip_amended"
	w9aEvtArrivalAmended = "arrival_amended"

	// trip_amended payload keys
	w9aKeyType            = "type"
	w9aKeyTripID          = "trip_id"
	w9aKeyDriver          = "driver_person_id"
	w9aKeyVehicle         = "vehicle_entered"
	w9aKeyDestination     = "destination"
	w9aKeyDestKind        = "kind"
	w9aKeyDestValue       = "value"
	w9aKeyDeadlineMinutes = "deadline_minutes"
	w9aKeyCargoNote       = "cargo_note"
	w9aKeyOverrideReason  = "phase_override_reason"
	w9aKeyOccupantsAdd    = "occupants_add"
	w9aKeyOccupantsRemove = "occupants_remove"
	w9aKeyClear           = "clear"

	// arrival_amended payload keys
	w9aKeyArrivalID  = "arrival_id"
	w9aKeyOriginSite = "origin_site"

	// destination kinds (F4 §3.1's DestinationAmend.Kind)
	w9aDestControlled   = "controlled"
	w9aDestUncontrolled = "uncontrolled"

	// side tables (F4 §3.2)
	w9aTblTripAmendment    = "trip_amendment"
	w9aTblArrivalAmendment = "arrival_amendment"

	// the fact-name PATTERN (F4 §6) — a prefix, not a member of a static list
	w9aFactOccupantPrefix = "occupant/"
)

// w9aClearWhitelist is §3.4/§3.3a's whitelist, VERBATIM and in full:
// `Clear` is legal for exactly these two nullable prose facts, and refused
// for destination/driver/vehicle (D-4). W9a-17 asserts that composition
// FILTERS to this set rather than refusing `Clear` outright.
var w9aClearWhitelist = []string{w9aKeyCargoNote, w9aKeyOverrideReason}

// w9aBase is the fixture's fixed audit "now" — w3Base's twin, kept
// separate so a W9a fixture never depends on another chunk's constant
// drifting.
const w9aBase = int64(1_800_000_000_000)

const w9aMinute = int64(60_000)

func w9aStr(s string) *string { return &s }
func w9aI64(v int64) *int64   { return &v }

// --- stores ---------------------------------------------------------------

// w9aNewStore opens a registered store with the clock parked at w9aBase.
func w9aNewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w9aReading(w9aBase)
	return st
}

func w9aReading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// w9aRawDB opens a SECOND handle on the store's file. Used for two things
// only: structural introspection (pragma_table_info / sqlite_master) and
// W9a-17's DIVERGENT-WRITER path, which by definition cannot go through an
// authoring surface. `Store.db` is unexported on purpose (C1 review M2), so
// this is the in-tree technique `w5d_sidetable_structural_test.go`
// established rather than a new backdoor.
func w9aRawDB(t *testing.T, st *store.Store) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatalf("w9a: opening a second handle on %s: %v", st.Path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- the wire path --------------------------------------------------------

// w9aEnvelope builds ONE replicated envelope by hand. It is the only way a
// `trip_amended` can exist before an authoring surface does, and it is
// deliberately the WIRE shape: the payload map's keys are F4 §3.1's frozen
// literals, so a builder that spells one of them differently reds every
// fixture that carries it.
//
// TimeSource is "default" and OccurredAt == AuthoredAt.WallMS, the rule-W
// equality — an envelope that violated it would be a fixture defect, not a
// W9a finding.
func w9aEnvelope(siteID string, epoch, seq, wallMS int64, payload map[string]any) store.Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("w9a: marshalling a fixture payload: %v", err))
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

// w9aTripAmend is the trip_amended payload builder. Every field is optional
// (nil = not amended, F4 §3.1); the map carries ONLY the keys the caller
// set, so `omitempty`'s wire meaning is reproduced exactly.
type w9aTripAmend struct {
	TripID          string
	Driver          *string
	Vehicle         *string
	Destination     *w9aDestination
	CargoNote       *string
	OverrideReason  *string
	OccupantsAdd    []string
	OccupantsRemove []string
	Clear           []string
}

type w9aDestination struct {
	Kind            string
	Value           string
	DeadlineMinutes *int64
}

func (a w9aTripAmend) payload() map[string]any {
	m := map[string]any{
		w9aKeyType:   w9aEvtTripAmended,
		w9aKeyTripID: a.TripID,
	}
	if a.Driver != nil {
		m[w9aKeyDriver] = *a.Driver
	}
	if a.Vehicle != nil {
		m[w9aKeyVehicle] = *a.Vehicle
	}
	if a.Destination != nil {
		d := map[string]any{
			w9aKeyDestKind:  a.Destination.Kind,
			w9aKeyDestValue: a.Destination.Value,
		}
		if a.Destination.DeadlineMinutes != nil {
			d[w9aKeyDeadlineMinutes] = *a.Destination.DeadlineMinutes
		}
		m[w9aKeyDestination] = d
	}
	if a.CargoNote != nil {
		m[w9aKeyCargoNote] = *a.CargoNote
	}
	if a.OverrideReason != nil {
		m[w9aKeyOverrideReason] = *a.OverrideReason
	}
	if len(a.OccupantsAdd) > 0 {
		m[w9aKeyOccupantsAdd] = a.OccupantsAdd
	}
	if len(a.OccupantsRemove) > 0 {
		m[w9aKeyOccupantsRemove] = a.OccupantsRemove
	}
	if len(a.Clear) > 0 {
		m[w9aKeyClear] = a.Clear
	}
	return m
}

// w9aArrivalAmend is the arrival_amended payload builder (F4 §3.1). It
// carries NO visit field and NO trip_ref: the un-reference is W9b's event
// and this payload structurally cannot re-point an arrival.
type w9aArrivalAmend struct {
	ArrivalID  string
	Vehicle    *string
	OriginSite *string
}

func (a w9aArrivalAmend) payload() map[string]any {
	m := map[string]any{
		w9aKeyType:      w9aEvtArrivalAmended,
		w9aKeyArrivalID: a.ArrivalID,
	}
	if a.Vehicle != nil {
		m[w9aKeyVehicle] = *a.Vehicle
	}
	if a.OriginSite != nil {
		m[w9aKeyOriginSite] = *a.OriginSite
	}
	return m
}

// w9aApply applies one wire envelope. Folds NEVER refuse (W3-2's rule,
// restated by F4 §3.2's "the fold is a bare INSERT and nothing else"), so a
// refusal here is a finding and is reported as one.
func w9aApply(t *testing.T, st *store.Store, env store.Envelope) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9a: apply %v (%s): %v — the fold REFUSED. F4 §3.2: the fold is a bare INSERT, it never validates against current state, and a dangling amendment must fold inert",
			env.Key(), w9aPayloadType(env), err)
	}
	if !applied {
		t.Fatalf("w9a: apply %v: deduped unexpectedly", env.Key())
	}
}

func w9aPayloadType(env store.Envelope) string {
	var m map[string]any
	if err := json.Unmarshal(env.Payload, &m); err != nil {
		return "<unparseable payload>"
	}
	s, _ := m[w9aKeyType].(string)
	return s
}

// w9aPadStream inflates (siteID, epoch) with filler events until its
// watermark is exactly targetSeq-1, so the NEXT applied envelope lands on
// targetSeq. The LOCKED fixtures of W9a-4 and W9a-5 state their keys as
// literals; this is how those literals are met, and it is the in-tree
// technique `w4_resolution_order_test.go` established for the same reason.
//
// The filler is a company creation: a real, folding, harmless event.
func w9aPadStream(t *testing.T, st *store.Store, siteID string, epoch, targetSeq int64) {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if wm >= targetSeq {
		t.Fatalf("w9a FIXTURE DEFECT: stream (%s, epoch %d) is already at watermark %d, past the locked key seq %d — the fixture's keys are LITERALS and cannot be met",
			siteID, epoch, wm, targetSeq)
	}
	for i := wm; i < targetSeq-1; i++ {
		if _, err := CreateCompany(st, siteID, epoch, fmt.Sprintf("W9a Filler %d", i), 0, "default"); err != nil {
			t.Fatalf("w9a: padding (%s, epoch %d) to seq %d: %v", siteID, epoch, targetSeq, err)
		}
	}
}

// w9aApplyAt pads the stream and applies the payload at the LOCKED key,
// then verifies the key literally — a fixture whose keys drifted would make
// every ordering assertion above it meaningless while still going green.
func w9aApplyAt(t *testing.T, st *store.Store, siteID string, epoch, seq, wallMS int64, payload map[string]any) store.Key {
	t.Helper()
	w9aPadStream(t, st, siteID, epoch, seq)
	env := w9aEnvelope(siteID, epoch, seq, wallMS, payload)
	if env.Key() != (store.Key{SiteID: siteID, NodeEpoch: epoch, Seq: seq}) {
		t.Fatalf("w9a FIXTURE DEFECT: built %v, want the LOCKED literal (%s, epoch %d, seq %d)", env.Key(), siteID, epoch, seq)
	}
	w9aApply(t, st, env)
	return env.Key()
}

// w9aApplyNext applies at the stream's NEXT free seq. Used where the
// criterion pins no key literal — pinning one there would make the fixture
// brittle against whatever the fold has already authored on that stream.
func w9aApplyNext(t *testing.T, st *store.Store, siteID string, epoch, wallMS int64, payload map[string]any) store.Key {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	env := w9aEnvelope(siteID, epoch, wm+1, wallMS, payload)
	w9aApply(t, st, env)
	return env.Key()
}

// w9aLogPayloadAt reads the payload of the event at key straight from the
// replicated LOG. It exists so a fixture can assert its own keys without
// depending on a side table the builder has not written yet — otherwise
// every ordering criterion reds at its precondition and its real assertion
// is never reached, and an unreached assertion has never had its failure
// mode produced.
func w9aLogPayloadAt(t *testing.T, st *store.Store, k store.Key) map[string]any {
	t.Helper()
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		t.Fatalf("w9a: reading the log at %v: %v", k, err)
	}
	if len(evs) != 1 || evs[0].Key() != k {
		t.Fatalf("w9a FIXTURE DEFECT: the log holds no event at %v", k)
	}
	var m map[string]any
	if err := json.Unmarshal(evs[0].Payload, &m); err != nil {
		t.Fatalf("w9a: payload at %v is not a JSON object: %v", k, err)
	}
	return m
}

// --- seeding entities with CHOSEN ids ------------------------------------
//
// The LOCKED fixtures name their people as literals (`drv-okafor`,
// `p-brand`, …) and W9a-5's `Outcome` half needs the FACT NAME
// `occupant/<raw person id>` to be that literal too. CreatePerson mints an
// id, so the roster row is seeded through the wire instead — the same
// hand-built-envelope path the amendments use.

func w9aSeedPerson(t *testing.T, st *store.Store, siteID string, epoch int64, personID, name string) string {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	env := w9aEnvelope(siteID, epoch, wm+1, w9aBase, map[string]any{
		"type": "person_created", "person_id": personID, "name": name,
	})
	w9aApply(t, st, env)
	return personID
}

// w9aSeedPersons seeds one roster row per id, giving each a display NAME
// equal to its id. That is not decoration: it makes every rendered
// occupant/driver name a LITERAL the criteria already pin, so a rendered
// assertion can compare against `drv-okafor` and not against a value the
// test computed.
func w9aSeedPersons(t *testing.T, st *store.Store, siteID string, epoch int64, ids ...string) {
	t.Helper()
	for _, id := range ids {
		w9aSeedPerson(t, st, siteID, epoch, id, id)
	}
}

// --- observing the EFFECTIVE record at the §9.2 consumers ----------------

// w9aExpectationFor returns the OpenExpectations row for tripID at siteID,
// or fails. OpenExpectations is F4 §9.1's named COMPOSED read projection —
// the shared input of the deadline scanner, the inbound console manifest and
// the /trips board rows (§9.2 consumers 2, 4, 5).
func w9aExpectationFor(t *testing.T, st *store.Store, siteID, tripID string) Expectation {
	t.Helper()
	es, err := OpenExpectations(st, siteID)
	if err != nil {
		t.Fatalf("w9a: OpenExpectations(%s): %v", siteID, err)
	}
	for _, e := range es {
		if e.Trip.TripID == tripID {
			return e
		}
	}
	t.Fatalf("w9a: OpenExpectations(%s) holds no row for trip %s — it holds %s", siteID, tripID, w9aExpectationIDs(es))
	return Expectation{}
}

func w9aExpectationIDs(es []Expectation) string {
	if len(es) == 0 {
		return "nothing"
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Trip.TripID)
	}
	return strings.Join(out, ", ")
}

// w9aOccupantNames is the ordered, sorted display-name set of an
// Expectation's manifest — the value W9a-5 pins as a literal set.
func w9aOccupantNames(e Expectation) []string {
	out := make([]string, 0, len(e.OccupantList))
	for _, o := range e.OccupantList {
		out = append(out, o.Name)
	}
	sort.Strings(out)
	return out
}

// w9aRawOccupantIDs is the RAW `trip_occupant` membership — the value that
// must NOT move when an amendment is folded (F4 §3.2: the fold is a bare
// INSERT).
func w9aRawOccupantIDs(t *testing.T, st *store.Store, tripID string) []string {
	t.Helper()
	os_, err := TripOccupantsByTrip(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(os_))
	for _, o := range os_ {
		out = append(out, o.PersonID)
	}
	sort.Strings(out)
	return out
}

func w9aSortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func w9aEqualStrings(a, b []string) bool {
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

// --- raw-row byte identity ------------------------------------------------

// w9aSection is one projection's canonical bytes. Two calls that differ are
// a real difference in that table's contents, in the store's own canonical
// encoding ([[D-56]]: never through an HTTP dump).
func w9aSection(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	b, err := st.DumpProjection(name)
	if err != nil {
		t.Fatalf("w9a: DumpProjection(%q): %v", name, err)
	}
	return string(b)
}

func w9aAllProjections(t *testing.T, st *store.Store) string {
	t.Helper()
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("w9a: DumpProjections: %v", err)
	}
	return string(b)
}

// --- fixture worlds -------------------------------------------------------

// w9aDispatchOpen dispatches an ordinary controlled trip from origin to
// destination with deadlineMinutes, and returns it. The trip is OPEN.
func w9aDispatchOpen(t *testing.T, st *store.Store, origin, destination string, epoch int64, vehicle, driver string, occupants []string, deadlineMinutes int64) Trip {
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
		DestinationSite:   &dest, DeadlineMinutes: w9aI64(deadlineMinutes),
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: dispatch: %v", err)
	}
	return trip
}

// w9aArriveExpected closes a trip with an expected arrival recorded at the
// destination — the "mis-click" of T15 and the ordinary close of T1/T2.
func w9aArriveExpected(t *testing.T, st *store.Store, siteID string, epoch int64, tripID string) Arrival {
	t.Helper()
	a, err := RecordExpectedArrival(st, siteID, epoch, tripID, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: expected arrival for %s: %v", tripID, err)
	}
	return a
}

func w9aTripState(t *testing.T, st *store.Store, tripID string) string {
	t.Helper()
	s, err := TripStateByID(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatalf("w9a: trip %s has no trip_state row", tripID)
	}
	return s.State
}
