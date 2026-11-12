// w9a_consumers_test.go — W9a's consumer- and immunity-shaped criteria that
// live in `package domain`:
//
//	W9a-3  (T3)  visits untouched — the `visit` TABLE half, locally and at
//	             the peer via the Envelope wire seam
//	W9a-7  (T7 / T7b) correlation immunity, THREE halves, each named
//	W9a-16 (T16) (a) the scoped-read probe for the TRIP reader
//	W9a-18 (T19) one human, one row — plus the raw wire and the un-merge
//
// The rendered halves of W9a-3, W9a-7 and W9a-18 live in internal/web.
//
// ⚠ THREE OF THESE PASS ON THE PRE-CHUNK TREE, and each says so in its own
// doc comment with the plant that would falsify it. Where a green could be
// mistaken for coverage, the test carries a POSITIVE CONTROL that is
// produced on EVERY run — an arm that demonstrates the judgement can report
// the other answer. An un-controlled green is not coverage (W6's gate).
package domain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/store"
)

// =========================================================================
// W9a-3 (T3) — visits untouched, the TABLE half
// =========================================================================

// TestW9a3AmendmentsNeverTouchTheVisitTable — W9a-3's `visit`-table half,
// asserted locally AND at a peer store fed the same envelopes through the
// Envelope wire seam.
//
// ⟦NO PRE-CHUNK RED⟧ — on the pre-chunk tree an amendment folds nothing, so
// "the visit table did not move" is trivially true. THE PLANT THAT WOULD
// FALSIFY IT: (m3) the HELPFUL FOLD — an `applyTripAmended` that closes the
// added occupant's open visit, or reopens the removed occupant's
// dispatch-closed one. That is the fold I-1 forbids: "an amendment authored
// at the peer whose fold mutated the origin's visit rows would put a foreign
// stream's write on another site's visit set, destroying the determinism
// argument".
//
// ⚠ The green is CONTROLLED on every run. Before the real assertion the test
// plants a visit change through a raw handle on a SCRATCH store and requires
// the same comparison to report it. Without that arm, a comparison that
// always returned "identical" would pass this test forever.
//
// The fixture is the criterion's: the REMOVED occupant X holds a
// dispatch-CLOSED origin visit; the ADDED occupant Z holds an OPEN one — so
// a helpful fold has something to touch in BOTH directions.
func TestW9a3AmendmentsNeverTouchTheVisitTable(t *testing.T) {
	// --- CONTROL FIRST: the comparison can report a difference ----------
	{
		scratch := w9aNewStore(t)
		w9aSeedPersons(t, scratch, "site-a", 1, "p-ctl")
		w9aCheckInAt(t, scratch, "site-a", 1, "p-ctl")
		before := w9aSection(t, scratch, "visit")
		db := w9aRawDB(t, scratch)
		if _, err := db.Exec(`UPDATE visit SET is_open = 0`); err != nil {
			t.Fatalf("W9a-3 CONTROL: could not plant a visit change: %v", err)
		}
		if after := w9aSection(t, scratch, "visit"); after == before {
			t.Fatalf("W9a-3 CONTROL FAILED: the `visit` section read IDENTICAL after a planted row change. "+
				"The comparison this test rests on cannot detect a visit mutation, so its clean verdict proves nothing.\nbefore: %s\nafter:  %s", before, after)
		}
	}

	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "p-x", "p-z", "p-driver")
	// Z is on site: an OPEN origin visit, and Z is NOT on the trip.
	w9aCheckInAt(t, st, "site-a", 1, "p-z")
	// X departs on the trip: dispatch CLOSES X's origin visit.
	w9aCheckInAt(t, st, "site-a", 1, "p-x")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-03", "p-driver", []string{"p-x"}, 240)

	closed, open := w9aVisitOpenState(t, st, "p-x"), w9aVisitOpenState(t, st, "p-z")
	if closed != 0 || open != 1 {
		t.Fatalf("PRECONDITION (W9a-3): the fixture must carry a dispatch-CLOSED visit for the removed occupant and an OPEN one for the added one; "+
			"p-x.is_open=%d (want 0), p-z.is_open=%d (want 1). A helpful fold would have nothing to touch in one direction", closed, open)
	}

	visitsBefore := w9aSection(t, st, "visit")

	amendments := []store.Envelope{}
	for _, p := range []w9aTripAmend{
		{TripID: trip.TripID, OccupantsRemove: []string{"p-x"}},
		{TripID: trip.TripID, OccupantsAdd: []string{"p-z"}},
	} {
		wm, err := st.Watermark("site-b", 1)
		if err != nil {
			t.Fatal(err)
		}
		env := w9aEnvelope("site-b", 1, wm+1, w9aBase+w9aMinute, p.payload())
		w9aApply(t, st, env)
		amendments = append(amendments, env)
	}

	if got := w9aSection(t, st, "visit"); got != visitsBefore {
		t.Fatalf("W9a-3 (m3 — the helpful fold): the `visit` table CHANGED across a %s / %s pair, LOCALLY.\nbefore: %s\nafter:  %s\n"+
			"F4 §3.2 / I-1: the fold never touches the `visit` table. F4 §7: no trip-fact correction ever adds or removes a person "+
			"from who-is-shown-on-site — presence is corrected only through the sanctioned visit-side paths",
			w9aEvtTripAmended, w9aEvtTripAmended, visitsBefore, got)
	}

	// --- and at the PEER, over the Envelope wire seam -------------------
	peer := w9aNewStore(t)
	w9aReplayInto(t, peer, st, []streamRef{{"site-a", 1}})
	peerBefore := w9aSection(t, peer, "visit")
	for _, env := range amendments {
		w9aApply(t, peer, env)
	}
	if got := w9aSection(t, peer, "visit"); got != peerBefore {
		t.Fatalf("W9a-3 (m3): the `visit` table CHANGED at the PEER across the same amendments delivered over the wire.\nbefore: %s\nafter:  %s\n"+
			"This is the direction I-1 names: a foreign stream's write landing on another site's visit set", peerBefore, got)
	}
	if peerBefore != visitsBefore {
		t.Fatalf("PRECONDITION (W9a-3): the peer's `visit` table does not match the origin's before any amendment — the wire seam did not reproduce the fixture.\norigin: %s\npeer:   %s",
			visitsBefore, peerBefore)
	}
}

// =========================================================================
// W9a-7 (T7 / T7b) — correlation immunity, THREE halves
// =========================================================================

// TestW9a7CorrelationImmunityTripHalfOnACancelledFixture — W9a-7's ⟨trip half⟩.
//
// KILLER MUTANT: (m7) the RETROACTIVE RE-BIND, trip half — a build that
// feeds the composed vehicle into `computeStates`' Pass-2 candidacy.
//
// ⚠ THE CLOSED STATE IS `cancelled`, AND THAT IS LOAD-BEARING: an *arrived*
// trip is excluded from Pass-2 candidacy by the `boundTo` filter before any
// vehicle comparison runs, so an arrived fixture cannot redden the mutant.
//
// ⟦NO PRE-CHUNK RED⟧ — nothing composes yet, so the corrected string matches
// nothing and lands `ArrivalPlain` for the wrong reason. THE PLANT: feed the
// effective vehicle into `computeStates`. ⚠ CONTROLLED ON EVERY RUN: the
// same fixture's second arrival types the ORIGINAL string and MUST land
// `ArrivalCancelledMatch`, proving this fixture can produce that disposition
// and that the negative above is not a fixture that could never match.
func TestW9a7CorrelationImmunityTripHalfOnACancelledFixture(t *testing.T) {
	const original, corrected = "TRK CANCEL 1", "TRK CORRECTED 1"
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "p-drv", "p-rider")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, original, "p-drv", []string{"p-rider"}, 240)
	if err := CancelTrip(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: cancel: %v", err)
	}
	if got := w9aTripState(t, st, trip.TripID); got != TripStateCancelled {
		t.Fatalf("PRECONDITION (W9a-7 trip half): trip is %q, want %q — the CANCELLED state is the fixture's load-bearing parameter", got, TripStateCancelled)
	}

	w9aApplyNext(t, st, "site-b", 1, w9aBase+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Vehicle: w9aStr(corrected),
	}.payload())

	// --- the assertion: the CORRECTED string must NOT match ------------
	arr := w9aUnsolicited(t, st, "site-b", 1, corrected, "site-a", []string{"p-rider"})
	if got := w9aArrivalDisposition(t, st, arr.ArrivalID); got != ArrivalPlain {
		t.Fatalf("W9a-7 (m7 — the retroactive re-bind, trip half): an unsolicited arrival typing the CORRECTED vehicle %q landed %q, want %q. "+
			"F4 §5 rule 4: a vehicle correction never re-runs correlation, never binds, never un-binds — correlation reads the RAW value, always",
			corrected, got, ArrivalPlain)
	}

	// --- CONTROL, same fixture: the ORIGINAL string DOES match ---------
	ctl := w9aUnsolicited(t, st, "site-b", 1, original, "site-a", []string{"p-rider"})
	if got := w9aArrivalDisposition(t, st, ctl.ArrivalID); got != ArrivalCancelledMatch {
		t.Fatalf("W9a-7 CONTROL FAILED (trip half): an unsolicited arrival typing the ORIGINAL vehicle %q landed %q, want %q. "+
			"Without this arm the assertion above is satisfied by a fixture in which NOTHING could ever match, and its clean verdict proves nothing",
			original, got, ArrivalCancelledMatch)
	}
}

// TestW9a7CorrelationImmunityVehicleClaimHalf — W9a-7's ⟨vehicle-claim half⟩,
// asserted SEPARATELY AND BY NAME. Kills (m12).
//
// ⟦NO PRE-CHUNK RED⟧ (nothing composes). THE PLANT: `computeStates` reading
// the composed arrival CLAIM. ⚠ CONTROLLED on every run by the third arm.
func TestW9a7CorrelationImmunityVehicleClaimHalf(t *testing.T) {
	w9a7ClaimHalf(t, "vehicle-claim", func(arrivalID string) w9aArrivalAmend {
		return w9aArrivalAmend{ArrivalID: arrivalID, Vehicle: w9aStr(w9a7Vehicle)}
	}, w9a7WrongVehicle, w9a7Origin)
}

// TestW9a7CorrelationImmunityOriginClaimHalf — W9a-7's ⟨origin-claim half⟩,
// asserted SEPARATELY AND BY NAME. Kills (m12b).
//
// ⚠ THIS HALF IS THE REASON THE CRITERION SAYS "THREE": Pass 2 matches on
// BOTH `(origin, vehicle)`, so a build that reads the ORIGIN claim
// effectively while the vehicle stays raw is byte-identical-green on the
// vehicle half and still silences the alarm. Only this arm reddens it.
func TestW9a7CorrelationImmunityOriginClaimHalf(t *testing.T) {
	w9a7ClaimHalf(t, "origin-claim", func(arrivalID string) w9aArrivalAmend {
		return w9aArrivalAmend{ArrivalID: arrivalID, OriginSite: w9aStr(w9a7Origin)}
	}, w9a7Vehicle, w9a7WrongOrigin)
}

const (
	w9a7Vehicle      = "ICE TRUCK 7"
	w9a7WrongVehicle = "ICE TRUKC 7"
	w9a7Origin       = "site-a"
	w9a7WrongOrigin  = "site-z"
)

// w9a7ClaimHalf runs one arrival-claim half in BOTH delivery permutations:
// the amendment delivered AFTER its arrival, and BEFORE it (dangling).
func w9a7ClaimHalf(t *testing.T, half string, amend func(arrivalID string) w9aArrivalAmend, typedVehicle, typedOrigin string) {
	t.Helper()
	for _, perm := range []string{"amendment-after-arrival", "amendment-before-arrival"} {
		t.Run(perm, func(t *testing.T) {
			st := w9aNewStore(t)
			w9aSeedPersons(t, st, "site-a", 1, "p-drv7", "p-rider7")
			trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, w9a7Vehicle, "p-drv7", []string{"p-rider7"}, 1)

			// The arrival types a claim that matches NOTHING.
			arr := w9aUnsolicited(t, st, "site-b", 1, typedVehicle, typedOrigin, []string{"p-rider7"})
			if got := w9aArrivalDisposition(t, st, arr.ArrivalID); got != ArrivalPlain {
				t.Fatalf("PRECONDITION (W9a-7 %s half): the mistyped arrival landed %q, want %q — the fixture must start UNBOUND", half, got, ArrivalPlain)
			}
			if got := w9aTripState(t, st, trip.TripID); got != TripStateOpen {
				t.Fatalf("PRECONDITION (W9a-7 %s half): the trip is %q, want %q", half, got, TripStateOpen)
			}

			arrivalsBefore := w9aSection(t, st, "trip_arrival")
			tripStateBefore := w9aSection(t, st, "trip_state")
			arrivalStateBefore := w9aSection(t, st, "arrival_state")

			// The correction of the CLAIM. In the dangling permutation the
			// amendment is applied from a stream whose earlier events are
			// already in place, so ordering within the store is the only
			// thing that moves.
			w9aApplyNext(t, st, "site-c", 1, w9aBase+2*w9aMinute, amend(arr.ArrivalID).payload())

			if got := w9aArrivalDisposition(t, st, arr.ArrivalID); got != ArrivalPlain {
				t.Fatalf("W9a-7 (m12 / m12b — the arrival-claim re-bind, %s half): after correcting the claim the arrival's disposition moved to %q, want %q. "+
					"F4 §5 rule 4: the correction is RECORD-ONLY; it does not re-run trip matching", half, got, ArrivalPlain)
			}
			if got := w9aTripState(t, st, trip.TripID); got != TripStateOpen {
				t.Fatalf("W9a-7 (m12 / m12b, %s half): the trip closed to %q after a claim correction. THE LIVE CONSEQUENCE: the overdue vehicle's alert is silenced by a record edit", half, got)
			}
			for _, s := range []struct{ name, before string }{
				{"trip_arrival", arrivalsBefore},
				{"trip_state", tripStateBefore},
				{"arrival_state", arrivalStateBefore},
			} {
				if got := w9aSection(t, st, s.name); got != s.before {
					t.Fatalf("W9a-7 (%s half): `%s` is NOT byte-identical across the claim correction.\nbefore: %s\nafter:  %s", half, s.name, s.before, got)
				}
			}

			// --- CONTROL, same fixture: the correlation CAN bind -------
			// An arrival typing the trip's RAW vehicle and RAW origin binds
			// it. Without this arm every assertion above is satisfied by a
			// world in which nothing could correlate at all.
			ctl := w9aUnsolicited(t, st, "site-b", 1, w9a7Vehicle, w9a7Origin, []string{"p-rider7"})
			if got := w9aArrivalDisposition(t, st, ctl.ArrivalID); got != ArrivalCorrelated {
				t.Fatalf("W9a-7 CONTROL FAILED (%s half): an arrival typing the trip's RAW (%q, %q) landed %q, want %q. "+
					"The fixture cannot produce a binding at all, so its refusal to bind above proves nothing",
					half, w9a7Vehicle, w9a7Origin, got, ArrivalCorrelated)
			}
		})
	}
}

// =========================================================================
// W9a-16(a) (T16) — the scoped-read probe, TRIP reader
// =========================================================================

// The instrument is `w5d_scoped_read_test.go`'s, cloned as W9a-16(a)
// requires ("cloned from w5d_scoped_read_test.go … asserted the way W5d
// asserts it, never by timing"): a counting scalar SQL function plus a TEMP
// VIEW shadowing the side table, so production's own SQL runs against the
// view and the counter reports rows EMITTED.
//
// ⚠ WHAT IS MEASURED HERE IS DIFFERENT FROM W5d's, deliberately. W5d could
// name `CompanyByID`. W9a cannot name `EffectiveTrip` (it does not exist and
// naming it is a compile error, see w9a_seams_test.go). So the probe is
// taken around §9.2 CONSUMER 6, `NotTracked`, which F4 §3.3 names as the
// exact place the cost bites: "`NotTracked` already calls per-trip readers
// in a loop over the season's trips … so an unscoped `EffectiveTrip` is
// quadratic on the muster path". A scoped read decodes each trip's own rows
// once (T×k); an unscoped one decodes the whole table per trip (T×T×k).

var w9aRowsDecoded atomic.Int64

var w9aProbeOnce sync.Once

const w9aRowProbeFn = "w9a_rows_decoded_probe"

func w9aRegisterRowProbe(t *testing.T) {
	t.Helper()
	w9aProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(w9aRowProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w9aRowsDecoded.Add(1)
				return args[0], nil
			})
	})
}

// w9aInstallTripAmendmentProbeView shadows `trip_amendment` with a view of
// the same shape whose `cleared` column runs through the probe. `cleared`
// is chosen for W5d's reason: it appears in neither the WHERE nor the ORDER
// BY of a composition read, so the probe fires exactly once per EMITTED row
// rather than once per row scanned.
func w9aInstallTripAmendmentProbeView(t *testing.T, st *store.Store) {
	t.Helper()
	cols := w9aColumnsOf(t, st, w9aTblTripAmendment)
	if len(cols) == 0 {
		t.Fatalf("W9a-16(a): table %s does not exist, so the rows-decoded shadow view cannot be installed. "+
			"On the pre-chunk tree that IS this criterion's produced red — F4 §3.2 requires the side table and §3.3 requires the SCOPED reader over it",
			w9aTblTripAmendment)
	}
	sel := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == "cleared" {
			sel = append(sel, w9aRowProbeFn+"(cleared) AS cleared")
			continue
		}
		sel = append(sel, c)
	}
	err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TEMP VIEW " + w9aTblTripAmendment + " AS SELECT " +
			joinComma(sel) + " FROM main." + w9aTblTripAmendment)
		return err
	})
	if err != nil {
		t.Fatalf("W9a-16(a) CONTROL FAILED (instrument): could not install the rows-decoded shadow view over %s: %v", w9aTblTripAmendment, err)
	}
}

func w9aMeasureRowsDecoded(t *testing.T, fn func()) int64 {
	t.Helper()
	before := w9aRowsDecoded.Load()
	fn()
	return w9aRowsDecoded.Load() - before
}

// TestW9a16TripCompositionDecodesOnlyItsOwnTripsRows — W9a-16(a), trip half.
//
// KILLER MUTANT: (m27) the UNSCOPED COMPOSITION READ — a whole-table decode
// per call. It returns byte-identical values, composes an identical trip,
// and is invisible to every other test in the tree; here it decodes T times
// too many rows on the muster path, at the cutover, green.
func TestW9a16TripCompositionDecodesOnlyItsOwnTripsRows(t *testing.T) {
	w9aRegisterRowProbe(t)
	st := w9aNewStore(t)

	const trips, perTrip = 4, 3
	w9aSeedPersons(t, st, "site-r", 1, "p-drv16", "p-missing16")
	var ids []string
	for i := 0; i < trips; i++ {
		trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, fmt.Sprintf("TRK-16-%d", i), "p-drv16", []string{"p-missing16"}, 240)
		// The driver arrives and p-missing16 does not, so the trip enters
		// NotTracked (§9.2 consumer 6 reads arrived controlled trips whose
		// visit set omits an occupant). An arrival with NOBODY aboard is
		// refused by the authoring path, so the driver must land.
		if _, err := RecordExpectedArrivalExcluding(st, "site-b", 1, trip.TripID, []string{"p-missing16"}, nil, 0, "default"); err != nil {
			t.Fatalf("w9a fixture: expected arrival excluding: %v", err)
		}
		for j := 0; j < perTrip; j++ {
			w9aApplyNext(t, st, "site-c", 1, w9aBase+int64(j)*w9aMinute, w9aTripAmend{
				TripID: trip.TripID, CargoNote: w9aStr(fmt.Sprintf("note %d-%d", i, j)),
			}.payload())
		}
		ids = append(ids, trip.TripID)
	}
	if len(ids) != trips {
		t.Fatalf("PRECONDITION: %d trips, want %d", len(ids), trips)
	}

	w9aInstallTripAmendmentProbeView(t, st)

	total := int64(trips * perTrip)
	decoded := w9aMeasureRowsDecoded(t, func() {
		if _, err := NotTracked(st); err != nil {
			t.Fatalf("NotTracked: %v", err)
		}
	})
	scoped, unscoped := total, int64(trips)*total
	if decoded != scoped {
		msg := ""
		if decoded == unscoped {
			msg = " — that is the WHOLE TABLE once per trip: the composition read is unscoped"
		}
		t.Fatalf("W9a-16(a) (m27 — the unscoped composition read): NotTracked decoded %d %s rows over %d trips, want %d (each trip's own %d)%s. "+
			"F4 §3.3: the composed readers are the `PersonByID` shape — a SCOPED read, filtered in SQL, never a whole-table decode per call; "+
			"§9.2 consumer 6 calls them in a loop over the season's trips, so an unscoped read is QUADRATIC on the muster path",
			decoded, w9aTblTripAmendment, trips, scoped, perTrip, msg)
	}
}

// =========================================================================
// W9a-18 (T19) — one human, one row
// =========================================================================

// TestW9a18OneHumanOneRowInTheNotAccountedForSection — W9a-18's first half
// on §9.2 consumer 6, plus its second half (the raw wire and the un-merge).
//
// KILLER MUTANTS: (m29) the RAW OCCUPANT READ — `NotTracked` builds its
// aboard map from raw ids and never resolves the merge, so one human is
// listed twice, possibly under two names, on the surface [[D-6]] makes
// 100%-accountable; and the MIRROR mutant, compose-time resolution, which
// fixes the display and makes the fact name unstable across the un-merge.
//
// ⚠ The test carries BOTH routes into the double-count class, on purpose:
//   - the AMENDMENT route, which is the criterion's fixture and which is
//     W9's own addition;
//   - the DISPATCH route (both ids selected at dispatch, merged after),
//     which F4 §6 names as PRE-EXISTING — "an officer selecting both P1 and
//     P2 at dispatch and merging afterwards produces the same two rows
//     today". That arm is this test's PRODUCED PRE-CHUNK RED and it is also
//     the positive control for the amendment arm: it proves the one-row
//     judgement can report TWO on this surface.
func TestW9a18OneHumanOneRowInTheNotAccountedForSection(t *testing.T) {
	for _, route := range []string{"dispatch-route (the pre-existing class — the CONTROL)", "amendment-route (W9's own addition)"} {
		t.Run(route[:strings.IndexByte(route, ' ')], func(t *testing.T) {
			st := w9aNewStore(t)
			w9aSeedPerson(t, st, "site-a", 1, "P1", "Jonas Pike")
			w9aSeedPerson(t, st, "site-a", 1, "P2", "J. Pike")
			w9aSeedPerson(t, st, "site-a", 1, "P-drv", "Driver Dee")

			occupants := []string{"P2"}
			if strings.HasPrefix(route, "dispatch") {
				occupants = []string{"P1", "P2"}
			}
			trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-19", "P-drv", occupants, 240)
			if strings.HasPrefix(route, "amendment") {
				w9aApplyNext(t, st, "site-a", 2, w9aBase+w9aMinute, w9aTripAmend{
					TripID: trip.TripID, OccupantsAdd: []string{"P1"},
				}.payload())
			}
			// The DRIVER arrives; the passengers do not, so they land in
			// the not-accounted-for section. (An arrival with nobody aboard
			// is refused by the authoring path, and only RAW occupants can
			// be marked as stayed behind — on the amendment route P1 is not
			// one, which is the whole point of that route.)
			stayed := []string{"P2"}
			if strings.HasPrefix(route, "dispatch") {
				stayed = []string{"P1", "P2"}
			}
			if _, err := RecordExpectedArrivalExcluding(st, "site-b", 1, trip.TripID, stayed, nil, 0, "default"); err != nil {
				t.Fatalf("w9a fixture: expected arrival excluding: %v", err)
			}
			// post B merges P1 into P2 — ordinary, legal, and nothing to do
			// with either the trip or the amendment.
			if _, err := AmendPerson(st, "site-b", 1, PersonAmendment{
				PersonID: "P1", SameAs: w9aStr("P2"),
			}, 0, "default"); err != nil {
				t.Fatalf("w9a fixture: merging P1 into P2: %v", err)
			}

			rows, err := NotTracked(st)
			if err != nil {
				t.Fatalf("NotTracked: %v", err)
			}
			var persons []OccupantRef
			for _, r := range rows {
				if r.TripID == trip.TripID {
					persons = r.Persons
				}
			}
			seen := map[string]int{}
			for _, p := range persons {
				switch p.PersonID {
				case "P1", "P2":
					seen["the human"]++
				}
			}
			if seen["the human"] != 1 {
				t.Fatalf("W9a-18 (m29 — the raw occupant read): the not-accounted-for section renders %d rows for ONE human (%+v) via the %s. "+
					"F4 §7 / [[D-59]]: occupant membership composes over RAW ids and the READ PATH resolves them through the merge map before these sections render, "+
					"so a legally merged person is not listed twice, possibly under two names, on the surface [[D-6]] makes 100%%-accountable. "+
					"`LoadPersonMergeMap` has no caller in this path today",
					seen["the human"], persons, route)
			}
		})
	}
}

// TestW9a18RawWireAndTheUnMergeKeepTheFactNameStable — W9a-18's second half.
//
// ⚠ This half kills the MIRROR mutant: compose-time resolution fixes the
// display and makes the fact name unstable across P-3 §5's un-merge,
// breaking `Outcome`'s per-fact supersession, which is keyed on fact-name
// equality.
func TestW9a18RawWireAndTheUnMergeKeepTheFactNameStable(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPerson(t, st, "site-a", 1, "P1", "Jonas Pike")
	w9aSeedPerson(t, st, "site-a", 1, "P2", "J. Pike")
	w9aSeedPerson(t, st, "site-a", 1, "P-drv", "Driver Dee")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-19b", "P-drv", []string{"P2"}, 240)
	key := w9aApplyNext(t, st, "site-a", 2, w9aBase+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, OccupantsAdd: []string{"P1"},
	}.payload())
	if _, err := AmendPerson(st, "site-b", 1, PersonAmendment{PersonID: "P1", SameAs: w9aStr("P2")}, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: merge: %v", err)
	}

	// --- the wire half: the side table still holds the RAW id ----------
	rows := w9aAmendmentRows(t, st, w9aTblTripAmendment)
	if len(rows) != 1 {
		t.Fatalf("W9a-18: %s holds %d rows, want 1", w9aTblTripAmendment, len(rows))
	}
	if got := rows[0].text(w9aKeyOccupantsAdd); got != "P1" {
		t.Fatalf("W9a-18: `%s.%s` reads %q, want the RAW id %q. [[D-59]]: the person ids inside occupants_add/occupants_remove are payload VALUES "+
			"and stay RAW — 'what a peer writes does not change', which is what keeps this clear of gate B-2's frozen wire semantics",
			w9aTblTripAmendment, w9aKeyOccupantsAdd, got, "P1")
	}

	// --- the un-merge half: the fact name does not move ----------------
	fact := w9aFactOccupantPrefix + "P1"
	before := w9aFact(t, st, key, fact)
	if _, err := AmendPerson(st, "site-b", 1, PersonAmendment{PersonID: "P1", Clear: []string{"same_as"}}, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: un-merge (P-3 §5): %v", err)
	}
	after := w9aFact(t, st, key, fact)
	if after.Fact != before.Fact || after.Fact != fact {
		t.Fatalf("W9a-18 (the MIRROR mutant — compose-time resolution): the fact name moved from %q to %q across P-3 §5's un-merge, want %q unchanged. "+
			"A fact name resolved at compose time becomes UNSTABLE across a later un-merge, and a fact name whose identity changes retroactively "+
			"breaks `Outcome`'s per-fact supersession, which is keyed on fact-name equality [[D-59]]",
			before.Fact, after.Fact, fact)
	}
	if after.Status != before.Status || after.Winner != before.Winner {
		t.Fatalf("W9a-18: `%s`'s supersession moved across the un-merge: %v/%v -> %v/%v", fact, before.Status, before.Winner, after.Status, after.Winner)
	}
}

// =========================================================================
// helpers used only by this file
// =========================================================================

type streamRef struct {
	site  string
	epoch int64
}

// w9aReplayInto copies the named streams from src into dst over the
// Envelope wire seam — the peer half of W9a-3.
func w9aReplayInto(t *testing.T, dst, src *store.Store, streams []streamRef) {
	t.Helper()
	for _, s := range streams {
		evs, err := src.Events(s.site, s.epoch, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			w9aApply(t, dst, e)
		}
	}
}

// w9aCheckInAt puts one person on site through the degenerate check-in
// pair, giving them an OPEN visit at siteID.
func w9aCheckInAt(t *testing.T, st *store.Store, siteID string, epoch int64, personID string) {
	t.Helper()
	trip, err := DispatchLocalCheckIn(st, siteID, epoch, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 1", DriverPersonID: personID,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "BUS 1"), DriverPersonID: personID,
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: local check-in dispatch for %s: %v", personID, err)
	}
	if _, err := RecordExpectedArrival(st, siteID, epoch, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: local check-in arrival for %s: %v", personID, err)
	}
}

func w9aVisitOpenState(t *testing.T, st *store.Store, personID string) int64 {
	t.Helper()
	vs, err := Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].PersonID == personID {
			return vs[i].IsOpen
		}
	}
	t.Fatalf("w9a fixture: no visit row for %s", personID)
	return -1
}

func w9aUnsolicited(t *testing.T, st *store.Store, siteID string, epoch int64, vehicle, origin string, personIDs []string) Arrival {
	t.Helper()
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "a, err := RecordUnsolicitedArrival(st, siteID, epoch, vehicle, origin, personIDs, nil, 0, "default")"⟧
	a, err := RecordUnsolicitedArrival(st, siteID, epoch, w7SeedVehicleID(t, st, siteID, vehicle), origin, personIDs, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: unsolicited arrival (%q from %q): %v", vehicle, origin, err)
	}
	return a
}

func w9aArrivalDisposition(t *testing.T, st *store.Store, arrivalID string) string {
	t.Helper()
	sts, err := ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sts {
		if s.ArrivalID == arrivalID {
			return s.Disposition
		}
	}
	t.Fatalf("w9a: arrival %s has no arrival_state row", arrivalID)
	return ""
}

// w9aColumnsOf lists a table's columns through a second handle, or returns
// nil when the table does not exist.
func w9aColumnsOf(t *testing.T, st *store.Store, table string) []string {
	t.Helper()
	db := w9aRawDB(t, st)
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func joinComma(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// =========================================================================
// W9a-16(b) (T16) — BOTH dispatch switches, extended TOGETHER
// =========================================================================

// TestW9a16BothDispatchSwitchesAreExtendedTogether — W9a-16's ⟨b⟩ half:
// "for EACH of the two new carriers, `Outcome` resolves it AND `Competitors`
// returns the POSITIVE answer (the competing keys for a contested fact), per
// W5d-11's shape".
//
// KILLER MUTANT: (m28) the HALF-REGISTERED CARRIER. There are TWO parallel
// `switch typ` dispatches, not one [MEASURED — outcome.go's `Outcome` and
// `Competitors`], and a carrier added to one and missed in the other DOES
// NOT FAIL LOUDLY: it falls to `outcomeMiss`, whose `IsRegisteredCarrier`
// check is TRUE, so it returns the SANCTIONED arm — "registered carrier %q
// at %v has no amendment record". A real event with a real side-table row is
// reported as having no record.
//
// ⚠ That is why this test asserts the POSITIVE answer and not merely the
// absence of an error: an enumeration that returns an empty map is
// indistinguishable from a correct answer on an uncontested fact, so the
// fixture makes BOTH facts contested and pins the competing key.
func TestW9a16BothDispatchSwitchesAreExtendedTogether(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "drv-16b-1", "drv-16b-2", "p-rider16b")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-16B", "drv-16b-1", []string{"p-rider16b"}, 240)

	// --- carrier 1: two competing `trip_amended` driver assertions ------
	tripLoser := w9aApplyNext(t, st, "site-a", 2, w9aBase+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-16b-1"),
	}.payload())
	tripWinner := w9aApplyNext(t, st, "site-b", 1, w9aBase+2*w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-16b-2"),
	}.payload())

	// --- carrier 2: two competing `arrival_amended` vehicle claims ------
	arr := w9aUnsolicited(t, st, "site-b", 2, "TRK-TYPO", "site-a", []string{"p-rider16b"})
	arrLoser := w9aApplyNext(t, st, "site-a", 3, w9aBase+3*w9aMinute,
		w9aArrivalAmend{ArrivalID: arr.ArrivalID, Vehicle: w9aStr("TRK-CLAIM-A")}.payload())
	arrWinner := w9aApplyNext(t, st, "site-b", 3, w9aBase+4*w9aMinute,
		w9aArrivalAmend{ArrivalID: arr.ArrivalID, Vehicle: w9aStr("TRK-CLAIM-B")}.payload())

	for _, c := range []struct {
		carrier       string
		fact          string
		loser, winner store.Key
	}{
		{w9aEvtTripAmended, w9aKeyDriver, tripLoser, tripWinner},
		{w9aEvtArrivalAmended, w9aKeyVehicle, arrLoser, arrWinner},
	} {
		t.Run(c.carrier, func(t *testing.T) {
			// (i) `Outcome` resolves it.
			out := w9aFact(t, st, c.loser, c.fact)
			if out.Status != Superseded || out.Winner != c.winner {
				t.Fatalf("W9a-16(b): Outcome(%v)[%q] = %v / winner %v, want Superseded / %v — the fixture makes this fact CONTESTED so the "+
					"competitor enumeration below has a positive answer to give", c.loser, c.fact, out.Status, out.Winner, c.winner)
			}
			// (ii) `Competitors` returns the POSITIVE answer for the same
			// fact — the half (m28) skips.
			comp, err := Competitors(st, c.loser)
			if err != nil {
				t.Fatalf("W9a-16(b) (m28 — the HALF-REGISTERED CARRIER): Outcome resolved %q at %v but Competitors REFUSED it: %v.\n"+
					"F4 §3.3: the two amendment carriers land in BOTH `switch typ` dispatches IN THE SAME CHANGE. A carrier added to one and missed "+
					"in the other falls to `outcomeMiss`, whose IsRegisteredCarrier check is TRUE, so a real event with a real side-table row is "+
					"reported as having NO RECORD — the exact disguise W5d's dispatch comment exists to prevent, reached by the one route that fix "+
					"does not cover", c.carrier, c.loser, err)
			}
			got := comp[c.fact]
			if len(got) == 0 {
				t.Fatalf("W9a-16(b) (m28): Competitors(%v) returns no competitor for the CONTESTED fact %q (got %v). An empty enumeration is "+
					"indistinguishable from a correct answer on an uncontested fact, which is why this fixture contests it", c.loser, c.fact, comp)
			}
			found := false
			for _, k := range got {
				if k == c.winner {
					found = true
				}
				if k == c.loser {
					t.Fatalf("W9a-16(b): Competitors(%v)[%q] contains the QUERIED key itself — the enumeration is of the OTHER events contesting the fact (W5d-9(b)'s shape)", c.loser, c.fact)
				}
			}
			if !found {
				t.Fatalf("W9a-16(b): Competitors(%v)[%q] = %v, want it to contain the winning key %v", c.loser, c.fact, got, c.winner)
			}
		})
	}
}
