// witness_y8_tripauthor_test.go — witness phase, y8-tripauthor assignment
// (reports/witness/_BRIEF.md). Kills census survivors Q5, Q6, Q7, Q8, Q18,
// all five in trip_amend_author.go's AmendTrip/AmendArrival refusal ladder.
//
// Every test here calls the ladder through its real entry point
// (AmendTrip / AmendArrival), not the unexported helpers, so what fails is
// the same thing an officer's POST would see. Where a plant makes the
// function return success with nothing recorded (Q8) or with a downstream
// effect that would still look fine to a bare err==nil check (Q18's
// structural union), the assertion goes one level deeper — a watermark
// count or a composed read — because "returns no error" is exactly the
// shape the mutant also produces.
//
// The extra tests below (X143, X303, X238, X320) are NOT census plants —
// the census (plants.py) never probed these sites. They are self-authored,
// same shape (line-anchored, exact text asserted before the edit), added
// while working outward through this file's 59 unreached blocks per the
// brief, prioritising ERR-ARM. All are reported in the witness report in
// the same line-anchored format so they can be folded into the census.
//
// X238 and X320 use the fault-injection seam y3-acks found and this pass
// initially missed: `Store.LocalTx` (internal/store/local.go:44, doc'd as
// "the sanctioned read/write path"), dropping `event_log` — the one table
// every `Author` call writes to, and no read in AmendTrip/AmendArrival's
// earlier rungs ever touches — so those rungs' reads keep succeeding right
// up to the write. See reports/witness/y3-faultseam.md and
// reports/witness/y8-tripauthor.md for the full trace.
package domain

import (
	"database/sql"
	"strings"
	"testing"

	"errors"
	msqlite "modernc.org/sqlite"
	"sitecontrol/internal/store"
)

// y8DropTable is w8_absences_test.go's w8Exec pattern (LocalTx, the
// sanctioned site-local read/write door) applied to a projection/log table
// instead — same call, same discipline, just not restricted to site-local
// tables here. Used only to force a targeted write failure; the store is
// always a fresh t.TempDir() one discarded at test end, so nothing is ever
// restored mid-test.
func y8DropTable(t *testing.T, st *store.Store, table string) {
	t.Helper()
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE " + table)
		return err
	}); err != nil {
		t.Fatalf("fixture defect: drop table %s: %v", table, err)
	}
}

// --- Q6: :139 'if strings.TrimSpace(in.TripID) == "" {' -> 'if false && ...' ---
//
// Rung 1 of the ladder (F4 §3.5): a blank TripID is an INPUT error with its
// own copy, "a trip must be named". If the plant is applied, rung 1 never
// fires and control falls through to TripByID, which finds no trip named ""
// and refuses on rung 3 instead — a DIFFERENT TripAmendInputError, with
// DIFFERENT officer-facing copy ("" is not a trip this post has a record
// of"). A bare err != nil check cannot see that difference; only the exact
// text can.
func TestWitnessY8Q6BlankTripIDRefusesWithItsOwnCopyNotRung3s(t *testing.T) {
	st := newDomainTestStore(t)
	const wantText = "a trip must be named"

	_, err := AmendTrip(st, "site-a", 1, TripAmendment{TripID: ""}, 0, "default")
	if err == nil {
		t.Fatal("AmendTrip accepted a blank TripID — rung 1 must refuse before any lookup runs")
	}
	if _, ok := err.(*TripAmendInputError); !ok {
		t.Fatalf("refusal is %T, want *TripAmendInputError: %v", err, err)
	}
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("refusal text = %q, want it to contain %q (Q6's plant leaves rung 1 disabled and rung 3's "+
			"\"is not a trip this post has a record of\" fires instead — a different sentence for a different "+
			"mistake)", err.Error(), wantText)
	}
}

// --- Q5: :174 'if in.assertsClassB() {' -> 'if false && in.assertsClassB() {' ---
//
// Rung 5, the Class-B state gate (§4): destination/deadline/vehicle cannot
// be amended while the trip is OPEN, because they drive live machinery. If
// the plant disables the gate, a vehicle correction on a still-open trip is
// authored instead of refused — the record would then disagree with the
// running countdown and the correlation key the doc warns about.
func TestWitnessY8Q5ClassBGateRefusesAVehicleAmendOnAnOpenTrip(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID:       w7SeedVehicleID(t, st, "site-a", "TRUCK 1"),
		DriverPersonID:  driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got, err := TripStateByID(st, trip.TripID); err != nil || got == nil || got.State != TripStateOpen {
		t.Fatalf("PRECONDITION: trip state = %+v, err=%v — want a freshly-dispatched OPEN trip", got, err)
	}
	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	newVehicleText := "TRUCK 1 (corrected)"
	_, err = AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: trip.TripID, VehicleEntered: &newVehicleText,
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendTrip accepted a vehicle correction on a trip that is still OPEN — the class-B gate " +
			"must refuse this while the deadline scanner and the correlation key still run off the original value")
	}
	if _, ok := err.(*TripAmendRefusal); !ok {
		t.Fatalf("refusal is %T, want *TripAmendRefusal: %v", err, err)
	}
	const wantText = "still open"
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("refusal text = %q, want it to contain %q", err.Error(), wantText)
	}
	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("refused but authored %d event(s) (watermark %d -> %d) — a refusal must record nothing",
			after-before, before, after)
	}
}

// --- Q18: :216 'if id == driver {' -> 'if false && id == driver {' ---
//
// Rung 8 (§3.4's legible local refusal): the driver may never be removed
// without a replacement named in the SAME correction. The structural union
// downstream would still keep the driver aboard even if this guard is
// bypassed (the doc's own "the structural union is the guarantee" line) —
// which means the officer-visible signal this guard protects is entirely
// the REFUSAL ITSELF: told here, in words, vs. silently authoring a
// same-shaped "success" that a later read would not even show as wrong.
func TestWitnessY8Q18DriverRemovalWithoutReplacementIsRefused(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	trip, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		VehicleID:      w7SeedVehicleID(t, st, "site-a", "gate walk-in"),
		DriverPersonID: driver.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	before, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: trip.TripID, OccupantsRemove: []string{driver.PersonID},
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendTrip removed the driver with no replacement named in the same correction, and returned " +
			"no error — the officer was never told, and an event was authored for a removal §3.4 forbids")
	}
	if _, ok := err.(*TripAmendRefusal); !ok {
		t.Fatalf("refusal is %T, want *TripAmendRefusal: %v", err, err)
	}
	const wantText = "is the recorded driver of this trip"
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("refusal text = %q, want it to contain %q", err.Error(), wantText)
	}
	after, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("refused but authored %d event(s) (watermark %d -> %d) — a refusal must record nothing",
			after-before, before, after)
	}
}

// --- Q7: :256 validateDestinationAmend — insert 'return nil' at the top ---
//
// Rung 7 (§3.4): a destination correction must be VALID WHOLE at authoring
// — a controlled destination that names no post is refused with its own
// sentence. This is the exact block the census measured at 0 of 15 blocks
// reached: no test in the project amends a destination at all. If the
// plant fires, EVERY destination amendment validates, including a
// controlled one that names nothing, and it lands in the trip_amendment
// side table for good.
func TestWitnessY8Q7InvalidDestinationAmendIsRefusedNotAuthored(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	dest := "site-b"
	deadline := int64(60)
	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID:       w7SeedVehicleID(t, st, "site-a", "TRUCK 1"),
		DriverPersonID:  driver.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Complete the trip so rung 5 (Class-B, Q5's own rung) does not shadow
	// rung 7 — this test is about validation, not about the open/closed
	// gate, so the fixture must be past it.
	if _, err := RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("record expected arrival: %v", err)
	}
	if got, err := TripStateByID(st, trip.TripID); err != nil || got == nil || got.State != TripStateArrived {
		t.Fatalf("PRECONDITION: trip state = %+v, err=%v — want ARRIVED so the class-B gate is not what refuses this", got, err)
	}

	_, err = AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: trip.TripID,
		// A controlled destination that names no post — invalid on its face.
		Destination: &DestinationAmend{Kind: "controlled", Value: ""},
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendTrip authored a controlled destination amendment that names no post — " +
			"validateDestinationAmend must refuse this before anything is recorded")
	}
	if _, ok := err.(*TripAmendRefusal); !ok {
		t.Fatalf("refusal is %T, want *TripAmendRefusal: %v", err, err)
	}
	const wantText = "a controlled destination must name the post"
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("refusal text = %q, want it to contain %q", err.Error(), wantText)
	}
	rows, rerr := readTripAmendmentsFor(st, trip.TripID)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(rows) != 0 {
		t.Fatalf("refused but the trip_amendment side table carries %d row(s) for this trip — a refusal must "+
			"record nothing, and %+v is exactly the "+
			"downstream fact a plant that skips validation would leave behind", len(rows), rows[0].Destination)
	}
}

// --- Q8: :298 insert 'return store.Envelope{}, nil' at the top of AmendArrival ---
//
// AmendArrival's own doc: "A refused amendment authors NOTHING — there is
// no half-recorded state." The Q8 plant is the worst shape on the census:
// it makes AmendArrival return (Envelope{}, nil) for EVERY call, before any
// validation and before any authoring. err == nil is exactly what a
// legitimate success also looks like, so the witness must look past the
// error value at whether the correction actually reached the read side —
// EffectiveArrival's Corrected flag and the arrival_amendment row count.
func TestWitnessY8Q8ArrivalAmendmentActuallyAuthorsAnEvent(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	arr, err := RecordUnsolicitedArrival(st, "site-a", 1,
		w7SeedVehicleID(t, st, "site-a", "TRUCK 9"), "site-b",
		[]string{driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatalf("record unsolicited arrival: %v", err)
	}
	if before, cerr := EffectiveArrival(st, arr.ArrivalID); cerr != nil || before == nil || before.Corrected {
		t.Fatalf("PRECONDITION: arrival already reads Corrected before any amendment (err=%v, %+v)", cerr, before)
	}
	beforeWM, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	newVehicleText := "TRUCK 9 (corrected)"
	_, err = AmendArrival(st, "site-a", 1, ArrivalAmendment{
		ArrivalID: arr.ArrivalID, VehicleEntered: &newVehicleText,
	}, 0, "default")
	if err != nil {
		t.Fatalf("expected a legitimate vehicle correction to succeed, got: %v", err)
	}

	afterWM, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if afterWM != beforeWM+1 {
		t.Fatalf("AmendArrival returned success (err == nil) but authored %d event(s) (watermark %d -> %d), "+
			"want exactly 1 — this is Q8's own shape: 'returns no error' is what a silent no-op ALSO produces",
			afterWM-beforeWM, beforeWM, afterWM)
	}
	rows, rerr := readArrivalAmendmentsFor(st, arr.ArrivalID)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one arrival_amendment row for %s, got %d", arr.ArrivalID, len(rows))
	}
	composed, cerr := EffectiveArrival(st, arr.ArrivalID)
	if cerr != nil {
		t.Fatal(cerr)
	}
	if composed == nil || !composed.Corrected {
		t.Fatalf("EffectiveArrival(%s).Corrected = %+v after a successful AmendArrival — the officer's read "+
			"would show this arrival as UNCORRECTED even though nothing refused the correction", arr.ArrivalID, composed)
	}
}

// =========================================================================
// Extra plants (X143, X303) — self-authored, working outward per the brief.
// =========================================================================

// --- X143 (self-authored): :143 'if err != nil {' -> 'if false && err != nil {' ---
// (the arm guarding TripByID's error, in AmendTrip)
//
// If TripByID fails for an internal reason (a broken read, not "no such
// trip"), rung 1's neighbour must propagate that fault AS a fault — an
// internal error, never one of the two officer-safe types (the file's own
// header rule: "anything else is an internal fault whose text must NEVER
// reach a page"). If this guard is skipped, `trip` stays nil (TripByID
// returns a nil pointer alongside its error) and rung 3 catches it instead,
// misreporting a real infrastructure fault as "not a trip this post has a
// record of" — a confidently wrong answer, not a missing one.
func TestWitnessY8ExtraX143TripByIDFaultIsNotMisreportedAsUnknownTrip(t *testing.T) {
	st := newDomainTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	txt := "TRUCK 1 (corrected)"
	_, err := AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: "trip-x", VehicleEntered: &txt,
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendTrip succeeded against a closed store — TripByID must fail here")
	}
	if _, ok := err.(*TripAmendInputError); ok {
		t.Fatalf("a TripByID FAULT surfaced as *TripAmendInputError (%v) — that type is officer-authored "+
			"copy for \"this record does not exist\", not for an internal read failure", err)
	}
	const wrongText = "is not a trip this post has a record of"
	if strings.Contains(err.Error(), wrongText) {
		t.Fatalf("refusal text = %q — it claims the trip is unknown, but the true cause is TripByID's own "+
			"error, which rung 1's neighbour must propagate instead of letting rung 3 relabel it", err.Error())
	}
	const wantPrefix = "domain: amend trip:"
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Fatalf("refusal text = %q, want it wrapped with %q (the internal-fault convention this file uses)", err.Error(), wantPrefix)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses).
	// database/sql exports no sentinel for a closed DB, so the chain is checked
	// at its ROOT instead: walked through every Unwrap, the innermost error
	// must be the raw database/sql fault, never one of our own prefixed wrappers.
	witnessRequireRawRoot(t, "X143", err)
}

// --- X303 (self-authored): :303 'if err != nil {' -> 'if false && err != nil {' ---
// (the arm guarding rawArrivalByID's error, in AmendArrival)
//
// Same shape as X143, one function over: if rawArrivalByID fails for an
// internal reason, this guard's removal lets `a == nil` catch it instead,
// misreporting the fault as "not an arrival this post has a record of".
func TestWitnessY8ExtraX303RawArrivalByIDFaultIsNotMisreportedAsUnknownArrival(t *testing.T) {
	st := newDomainTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	txt := "TRUCK 9 (corrected)"
	_, err := AmendArrival(st, "site-a", 1, ArrivalAmendment{
		ArrivalID: "arrival-x", VehicleEntered: &txt,
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendArrival succeeded against a closed store — rawArrivalByID must fail here")
	}
	if _, ok := err.(*TripAmendInputError); ok {
		t.Fatalf("a rawArrivalByID FAULT surfaced as *TripAmendInputError (%v) — that type is officer-authored "+
			"copy for \"this record does not exist\", not for an internal read failure", err)
	}
	const wrongText = "is not an arrival this post has a record of"
	if strings.Contains(err.Error(), wrongText) {
		t.Fatalf("refusal text = %q — it claims the arrival is unknown, but the true cause is rawArrivalByID's "+
			"own error, which this guard must propagate instead of letting the next check relabel it", err.Error())
	}
	const wantPrefix = "domain: amend arrival:"
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Fatalf("refusal text = %q, want it wrapped with %q (the internal-fault convention this file uses)", err.Error(), wantPrefix)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses).
	// database/sql exports no sentinel for a closed DB, so the chain is checked
	// at its ROOT instead: walked through every Unwrap, the innermost error
	// must be the raw database/sql fault, never one of our own prefixed wrappers.
	witnessRequireRawRoot(t, "X303", err)
}

// --- X238 (self-authored): :238 'if err != nil {' -> 'if false && err != nil {' ---
// (the arm guarding st.Author's error, in AmendTrip)
//
// This is the arm the report first called unwitnessable: reaching it needs
// every earlier rung to succeed against a WORKING store and then only the
// final write to fail. `event_log` is the one table `Author` ever writes to
// and no earlier rung's read (TripByID, TripStateByID, the roster check)
// touches it, so dropping it via LocalTx isolates exactly this split. If the
// plant fires, `AmendTrip` returns (env, nil) for a write that never
// happened — the file's own doc says "a refused amendment authors NOTHING",
// and this is the mirror hazard: an UNREFUSED amendment that authors
// nothing either, while reporting success.
func TestWitnessY8ExtraX238AuthorFaultIsNotSwallowedAsSuccess(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	trip, err := DispatchLocalCheckIn(st, "site-a", 1, DispatchInput{
		VehicleID:      w7SeedVehicleID(t, st, "site-a", "gate walk-in"),
		DriverPersonID: driver.PersonID,
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// A plain, valid, non-class-B correction — nothing else on the ladder
	// has any reason to refuse it.
	note := "load manifest attached at the gate"

	y8DropTable(t, st, "event_log")

	_, err = AmendTrip(st, "site-a", 1, TripAmendment{
		TripID: trip.TripID, CargoNote: &note,
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendTrip returned success with event_log dropped — the write cannot have happened, and " +
			"the officer was told it did")
	}
	if _, ok := err.(*TripAmendRefusal); ok {
		t.Fatalf("an Author FAULT surfaced as *TripAmendRefusal (%v) — that type is officer-authored copy "+
			"for a business rule, not an infrastructure failure", err)
	}
	if _, ok := err.(*TripAmendInputError); ok {
		t.Fatalf("an Author FAULT surfaced as *TripAmendInputError (%v) — same objection, different type", err)
	}
	const wantPrefix = "domain: amend trip:"
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Fatalf("error text = %q, want it wrapped with %q", err.Error(), wantPrefix)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED (PRODUCED):
	// "domain: amend trip:" is SHARED across five call sites in AmendTrip —
	// dropping `trip` instead of `event_log` here still passes this prefix
	// check (a DIFFERENT arm fires). Pin the assertion to the table this
	// fixture guarantees is gone.
	const wantCause = "no such table: event_log"
	if !strings.Contains(err.Error(), wantCause) {
		t.Fatalf("error text = %q, want it to name the dropped table (%q) — otherwise this witness cannot "+
			"tell this arm apart from AmendTrip's other four call sites sharing the same wrap prefix", err.Error(), wantCause)
	}
	// The side table is a DIFFERENT table from the one dropped — proof the
	// isolation is real, not a side effect of a broken store: the fold
	// never ran, because Author never got past its own watermark read.
	rows, rerr := readTripAmendmentsFor(st, trip.TripID)
	if rerr != nil {
		t.Fatalf("trip_amendment must still be queryable (only event_log was dropped): %v", rerr)
	}
	if len(rows) != 0 {
		t.Fatalf("AmendTrip refused (correctly) but the trip_amendment side table still carries %d row(s) — "+
			"a refusal, real or by fault, must record nothing", len(rows))
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "X238", err.Error())
	}
}

// --- X320 (self-authored): :320 'if err != nil {' -> 'if false && err != nil {' ---
// (the arm guarding st.Author's error, in AmendArrival)
//
// Same shape and same seam as X238, the sibling function over.
func TestWitnessY8ExtraX320AuthorFaultIsNotSwallowedAsSuccess(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	arr, err := RecordUnsolicitedArrival(st, "site-a", 1,
		w7SeedVehicleID(t, st, "site-a", "TRUCK 9"), "site-b",
		[]string{driver.PersonID}, nil, 0, "default")
	if err != nil {
		t.Fatalf("record unsolicited arrival: %v", err)
	}
	newVehicleText := "TRUCK 9 (corrected)"

	y8DropTable(t, st, "event_log")

	_, err = AmendArrival(st, "site-a", 1, ArrivalAmendment{
		ArrivalID: arr.ArrivalID, VehicleEntered: &newVehicleText,
	}, 0, "default")

	if err == nil {
		t.Fatal("AmendArrival returned success with event_log dropped — the write cannot have happened, and " +
			"the officer was told it did")
	}
	if _, ok := err.(*TripAmendRefusal); ok {
		t.Fatalf("an Author FAULT surfaced as *TripAmendRefusal (%v)", err)
	}
	if _, ok := err.(*TripAmendInputError); ok {
		t.Fatalf("an Author FAULT surfaced as *TripAmendInputError (%v)", err)
	}
	const wantPrefix = "domain: amend arrival:"
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Fatalf("error text = %q, want it wrapped with %q", err.Error(), wantPrefix)
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED (PRODUCED):
	// "domain: amend arrival:" is SHARED across three call sites in
	// AmendArrival, same shape as X238's sibling. Pin the assertion to the
	// table this fixture guarantees is gone.
	const wantCause = "no such table: event_log"
	if !strings.Contains(err.Error(), wantCause) {
		t.Fatalf("error text = %q, want it to name the dropped table (%q) — otherwise this witness cannot "+
			"tell this arm apart from AmendArrival's other two call sites sharing the same wrap prefix", err.Error(), wantCause)
	}
	rows, rerr := readArrivalAmendmentsFor(st, arr.ArrivalID)
	if rerr != nil {
		t.Fatalf("arrival_amendment must still be queryable (only event_log was dropped): %v", rerr)
	}
	if len(rows) != 0 {
		t.Fatalf("AmendArrival refused (correctly) but the arrival_amendment side table still carries %d "+
			"row(s) — a refusal, real or by fault, must record nothing", len(rows))
	}
	composed, cerr := EffectiveArrival(st, arr.ArrivalID)
	if cerr != nil {
		t.Fatal(cerr)
	}
	if composed != nil && composed.Corrected {
		t.Fatal("EffectiveArrival reports Corrected=true after an AmendArrival that could not have written anything")
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "X320", err.Error())
	}
}
