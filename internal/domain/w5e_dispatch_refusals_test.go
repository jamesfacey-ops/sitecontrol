// w5e_dispatch_refusals_test.go — W5e-3's BEHAVIOURAL half: "No new refusal
// on the dispatch path. Dispatch refuses exactly what it refused at 88dd6a6
// … A cargo-bearing and a cargo-less dispatch are both accepted."
// design/WINDOW-BUILD-PLAN-v1.md "#### W5e — item 3: cargo note", W5e-3,
// LOCKED 2026-08-31.
//
// This is the ACCEPTANCE side. The structural side — the pinned refusal
// literal list asserted equal — lives in
// internal/store/w5e_dispatch_refusal_census_test.go, which compiles and runs
// against the clean tree; this file cannot, because it names
// DispatchInput.CargoNote (SEAM W5e-S-EDGE).
//
// ⟦NO PRE-CHUNK RED⟧ for W5e-3 (F-10: "it pins the pre-chunk tree against
// itself"). Evidence is (m5e-3), plus the census's plant.
//
// EXPECTED RED:
//   - pre-chunk: COMPILE red on DispatchInput.CargoNote, with the rest of
//     internal/domain's W5e files.
//   - (m5e-3) "an empty cargo note rejected":
//     TestW5eEveryCargoValueIsAccepted, the "empty string" sub-test.
//   - (m5e-3) "length capped": the same test's "2 KB" and "8 KB" sub-tests.
//   - (m5e-3) any OTHER new arm, e.g. "a note is required for a departure":
//     the same test's "absent" sub-test, and the census in internal/store.
//   - a refusal REMOVED while adding the field (the other direction, which
//     "refuses EXACTLY what it refused" equally forbids):
//     TestW5eEveryPreChunkDispatchRefusalStillFires.
package domain

import (
	"strings"
	"testing"
)

// w5ePreChunkRefusalCases replays each refusal the dispatch path carried at
// 88dd6a6 that is REACHABLE FROM A CALLER. Two of the pinned arms are not
// reachable by construction and are named in the test below rather than
// silently omitted:
//   - the two trailing "domain: dispatch: %w" arms (json.Marshal failing, and
//     store.Author failing)
//
// ⟦was: "Three of the nineteen" — the third, "domain: dispatch: phase gate:
// %w", and the reachable "blocking weather phase" arm below both went with
// weather phases (James, 2026-10-04: "go ahead and delete the phase
// conditions")⟧
//
// Each of the two is an internal-error wrapper, not a rule about input, and
// each IS covered structurally by the pinned census in internal/store.
var w5ePreChunkRefusalCases = []struct {
	Name string
	Want string // substring of the refusal, drawn from the 88dd6a6 literal
	Run  func(t *testing.T, w *w5eWorld) error
}{
	// ⟦LOCK — §62, 2026-09-03⟧ ⚠⚠ THE NEXT TWO ARMS ARE DIFFERENT CLASSES AND ARE RULED ONE BY
	// ONE. §62's R1 site list names "the `"  "` / `"walk-in"` arms" together; §39 (`W7-B14`),
	// which is both earlier and more specific, splits them — and MEASUREMENT agrees with §39.
	// §39.1's own table: "(i) arm 2 struck as a RELOCATION … (ii) arm 1 kept, literal preserved
	// byte-for-byte", and §39.1 records that a two-arm form was considered and REJECTED because
	// it "would leave the locked census SHORT BY ONE against the CORRECT build".
	//
	// ARM 1 — KEPT, RE-POINTED AT THE ID. The guard did NOT move. [MEASURED —
	// `tripauthor.go:223-225`] the `== ""` refusal still stands at Dispatch, under a comment
	// reading "What moves to vehicle creation is the PLACEHOLDER refusal; the names-an-entity
	// refusal STAYS." §39.4(ii): "NOT struck. It is RE-POINTED at the `vehicle_id`, and THE
	// LITERAL IS PRESERVED BYTE-FOR-BYTE." Name unchanged, `Want` unchanged, layer unchanged —
	// only the field name moves, which is exactly what §39.6 booked as owed.
	// ⟦was: "in.VehicleEntered = \"  \""⟧
	{"empty vehicle id", "vehicle id is required", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.VehicleID = "  " })
	}},
	// ARM 2 — STRUCK, AND THE STRIKE IS A RELOCATION, NEVER A REMOVAL. [[D-11]]: "the
	// placeholder refusal STAYS." `DR-W7-5` / `W7-25(e)` moved it to `CreateVehicle`, which
	// `tripauthor.go:35` calls "the ONE place the placeholder list is consulted", and
	// `tripauthor.go:226-229` carries the removed line as its own ⟦was:⟧. So "Dispatch refuses
	// exactly what it refused at 88dd6a6" is FALSE for this arm and only this arm.
	//
	// ⚠⚠ §39.4(iv) permits this strike ONLY as ONE ACT with the arrival of its replacement — "a
	// strike recorded without naming the criterion that took the assertion over is how a guard
	// dies." THE REPLACEMENT EXISTS, AND THIS PASS WROTE IT:
	// `TestGateCheckInRefusesAPlaceholderVehicleID` (vehicle_placeholder_test.go:54) now calls
	// `CreateVehicle` over the whole of `placeholdersUnderTest` — "walk-in" among them — and
	// asserts THE SAME LITERAL, "does not identify a vehicle", byte-for-byte; its positive
	// control (`:102`) shows a legitimate number still accepted, so the guard is not vacuously
	// satisfied by refusing everything.
	//
	// ⚠ MEASURED BEFORE THE STRIKE, and it is why the one-act condition was not a formality:
	// `grep -rn "CreateVehicle" --include=*_test.go internal/` found NO behavioural assertion of
	// the relocated guard anywhere in the tree — only `w7SeedVehicleID`'s routing guard, labelled
	// "NOT AN ASSERTION" in its own file, and `W7-25(e)`, which is a SOURCE SCAN that never calls
	// `CreateVehicle`. Had this arm been struck without writing the replacement, the placeholder
	// refusal would have had NO behavioural cover at all.
	//
	// ⚠ THE STRUCK CASE IS PRESERVED HERE, NOT DELETED — it is the record of what this file used
	// to pin, and `W5e-3`'s criterion text is amended to match at its own point of use (§39.4(iii)).
	// ⟦was: "{\"placeholder vehicle id\", \"does not identify a vehicle\", func(t *testing.T, w *w5eWorld) error { return w5eDispatchErr(w, func(in *DispatchInput) { in.VehicleEntered = \"walk-in\" }) }},"⟧
	{"no driver", "driver is required", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.DriverPersonID = "" })
	}},
	{"both destinations", "cannot have both a controlled and an uncontrolled destination", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.DestinationUncontrolled = w5ePtr("pad-7") })
	}},
	{"departure with no destination (D-4)", "must record where it is going", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.DestinationSite = nil
			in.DeadlineMinutes = nil
		})
	}},
	{"uncontrolled destination with a deadline", "cannot be honoured", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.DestinationSite = nil
			in.DestinationUncontrolled = w5ePtr("pad-7")
		})
	}},
	{"destination with no deadline", "requires a deadline", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.DeadlineMinutes = nil })
	}},
	{"unknown peer destination", "is not a known peer site", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.DestinationSite = w5ePtr("site-zz")
			in.KnownDestinationSites = []string{"site-b"}
		})
	}},
	{"unconfigured uncontrolled destination", "is not a configured uncontrolled destination", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.DestinationSite = nil
			in.DeadlineMinutes = nil
			in.DestinationUncontrolled = w5ePtr("pad-zz")
			in.KnownUncontrolledDestinations = []string{"pad-7"}
		})
	}},
	{"non-positive deadline", "deadline must be a positive number of minutes", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.DeadlineMinutes = w5ePtrInt64(0) })
	}},
	// ⟦was: {"blocking weather phase with no override reason", "permits no
	// new departures", ...} — the phase gate was removed with weather phases,
	// James 2026-10-04⟧
	{"unknown driver under roster validation", "is not on the roster", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.ValidateRoster = true
			in.DriverPersonID = "site-a-person-ffffffffffffffff"
		})
	}},
	{"unknown occupant under roster validation", "is not on the roster", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) {
			in.ValidateRoster = true
			in.OccupantPersonIDs = []string{"site-a-person-ffffffffffffffff"}
		})
	}},
	{"empty occupant person id", "empty occupant person id", func(t *testing.T, w *w5eWorld) error {
		return w5eDispatchErr(w, func(in *DispatchInput) { in.OccupantPersonIDs = []string{""} })
	}},
	{"local check-in with a destination", "has no destination", func(t *testing.T, w *w5eWorld) error {
		_, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
			// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R3⟧ NOT
			// RE-SEEDED — and neither is any other site in this file. Every case runs inside
			// TestW5eEveryPreChunkDispatchRefusalStillFires, which takes
			// `before := len(w.events(w.a, "site-a"))` and then asserts the refusal "minted
			// nothing". A vehicle record IS an event, so re-seeding reddens EVERY case.
			// ⚠ And this file's subject is the relocated guard itself: two of its arms mutate
			// `in.VehicleEntered` to "  " and "walk-in", which `W7-25(e)` moved to CreateVehicle —
			// so "Dispatch must refuse exactly what it refused at 88dd6a6" has become FALSE for
			// those arms. PLAN OWNER's. See reports/window-build/RESEED-164.md §3.
			//
			// ⟦LOCK — §62, 2026-09-03⟧ R3 — THE EVENT COUNT, NOT THE MEANING. The note's
			// mechanism is exactly right and its two halves got two different dispositions: the
			// two guard arms are handled above (one re-pointed, one struck), and THIS site is the
			// fixture-mechanism half. ⚠ The assertion is still TRUE and still NEEDED, so nothing
			// about it moves: the vehicle is seeded in `w5eSeedCaseVehicles` BEFORE `before` is
			// captured, which puts the `vehicle_created` event OUTSIDE the measured window. The
			// call below is `w7SeedVehicleID`'s MEMOISED LOOKUP — it finds the record the seed
			// already made and authors nothing. ⛔ Re-baselining `before`/`after` was the tempting
			// repair and is BARRED; §62's condition is discharged in §5 of the report.
			// ⟦was: "VehicleEntered: \"gate walk-in 2\","⟧
			VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in 2"), DriverPersonID: w.driver.PersonID,
			DestinationSite: w5ePtr("site-b"),
		}, 0, "default")
		return err
	}},
	{"local check-in with a deadline", "has no deadline", func(t *testing.T, w *w5eWorld) error {
		_, err := DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
			// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R3⟧ NOT
			// RE-SEEDED — same window, same reason.
			// ⟦LOCK — §62, 2026-09-03⟧ R3, same disposition as the case above: seeded outside the
			// window in `w5eSeedCaseVehicles`, memoised lookup here, assertion untouched.
			// ⟦was: "VehicleEntered: \"gate walk-in 3\","⟧
			VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in 3"), DriverPersonID: w.driver.PersonID,
			DeadlineMinutes: w5ePtrInt64(30),
		}, 0, "default")
		return err
	}},
}

func w5ePtrInt64(v int64) *int64 { return &v }

// ⟦LOCK — §62, 2026-09-03⟧ w5eSeedCaseVehicles is §62 class R3's whole mechanism, and it is a
// FIXTURE ARRANGEMENT — it asserts nothing.
//
// ⚠ THE POINT IS WHERE IT IS CALLED, NOT WHAT IT DOES. Every case in this file runs inside a
// window opened by `before := len(w.events(w.a, "site-a"))`, and a `vehicle_created` event is an
// event, so a record seeded INSIDE that window reddens "the refusal minted nothing" without
// anything about the refusal having changed. §62: "seed the vehicle in the `w5eWorld` setup,
// BEFORE `before` is captured … the assertion is preserved EXACTLY, with no assertion changed and
// no expected value touched." That is what the two call sites below do.
//
// ⚠ IT IS CALLED IN THIS FILE RATHER THAN INSIDE `w5eNewWorld`. §62 names the world setup, and
// the world builder lives in `w5e_cargo_note_roundtrip_test.go` — NOT one of the seven files this
// pass owns, and other passes are live in this tree. The window is what the ruling is about, not
// the file: called on the first line after the world is built, the seed lands at exactly the same
// place in the stream it would have from inside the builder.
//
// ⚠ THE THREE NUMBERS ARE KEPT DISTINCT. Collapsing them onto one record would need one fewer
// seed, but `w7SeedVehicleID`'s own comment rules that out in terms: two dispatches carrying the
// same number are "two runs OF THE SAME TRUCK", so merging three trucks into one is a fixture
// MEANING change, which §47 condition 3 bars.
var w5eCaseVehicleNumbers = []string{"TRUCK 12", "gate walk-in 2", "gate walk-in 3"}

func w5eSeedCaseVehicles(t *testing.T, w *w5eWorld) {
	t.Helper()
	for _, number := range w5eCaseVehicleNumbers {
		w7SeedVehicleID(t, w.a, "site-a", number)
	}
}

// w5eDispatchErr runs one otherwise-VALID departure with the given mutation
// applied, and returns Dispatch's error.
func w5eDispatchErr(w *w5eWorld, mutate func(*DispatchInput)) error {
	in := DispatchInput{
		// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R3⟧ NOT
		// RE-SEEDED. This builder is what the "minted nothing" cases run through, and `mutate`
		// below is what rewrites this very field to the two values the relocated guard used to
		// refuse. PLAN OWNER's.
		//
		// ⟦LOCK — §62, 2026-09-03⟧ R3. ⚠ THE ROUTE'S SECOND SENTENCE IS WHY THIS SITE MATTERS
		// MOST: `mutate` rewrites this field, so the two guard arms and the fifteen ordinary
		// cases all pass through one line. Handling them per-file would have forced one
		// disposition onto both classes — §62 bar 1's trap, in a single struct literal. They got
		// two: arm 1 re-points this field to "  " (still refused HERE, at Dispatch), arm 2 is
		// struck to `CreateVehicle`, and the fifteen others need only the record to exist before
		// the window opens. Memoised lookup, no event, assertion and expected value untouched.
		// ⟦was: "VehicleEntered:    \"TRUCK 12\","⟧
		VehicleID:         w7SeedVehicleID(w.t, w.a, "site-a", "TRUCK 12"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   w5ePtr("site-b"),
		DeadlineMinutes:   w5ePtrInt64(60),
	}
	mutate(&in)
	_, err := Dispatch(w.a, "site-a", 1, in, 0, "default")
	return err
}

// TestW5eEveryPreChunkDispatchRefusalStillFires — W5e-3, the "exactly what it
// refused" direction that a set-equality census cannot produce on its own:
// the arms must still FIRE, not merely still be present in source.
func TestW5eEveryPreChunkDispatchRefusalStillFires(t *testing.T) {
	// Rule 6 — the arrangement: the UNMUTATED input must be ACCEPTED, or
	// every case below would "refuse" for a reason that has nothing to do
	// with the mutation under test.
	base := w5eNewWorld(t)
	if err := w5eDispatchErr(base, func(*DispatchInput) {}); err != nil {
		t.Fatalf("W5e-3 fixture defect: the unmutated departure was refused (%v) — every refusal case below would then be measured against the wrong cause", err)
	}

	for _, tc := range w5ePreChunkRefusalCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			w := w5eNewWorld(t)
			// ⟦LOCK — §62, 2026-09-03⟧ R3 — THE SEED GOES HERE, ONE LINE ABOVE THE WINDOW.
			// Every `w7SeedVehicleID` call reached from `tc.Run` below is then a MEMOISED
			// LOOKUP that authors nothing, so `after - before` is what it was at 88dd6a6.
			w5eSeedCaseVehicles(t, w)
			before := len(w.events(w.a, "site-a"))
			err := tc.Run(t, w)
			if err == nil {
				t.Fatalf("W5e-3: %q is NO LONGER REFUSED. Dispatch must refuse exactly what it refused at 88dd6a6 — no more AND no less; a refusal lost while adding a recorded field is the same defect as one gained", tc.Name)
			}
			if !strings.Contains(err.Error(), tc.Want) {
				t.Fatalf("W5e-3: %q was refused with %q, which does not carry the 88dd6a6 text %q — the refusal set is pinned by its literals", tc.Name, err.Error(), tc.Want)
			}
			// A refusal authors NOTHING: the refusal sits above the
			// authoring line, as every dispatch refusal did at 88dd6a6.
			if after := len(w.events(w.a, "site-a")); after != before {
				t.Fatalf("W5e-3: %q was refused but authored %d event(s) anyway — a refused departure must mint nothing", tc.Name, after-before)
			}
		})
	}

	// The two unreachable arms, named rather than silently skipped
	// (rule 5's own lesson: a check that cannot pose its case must say so).
	// ⟦was: "three internal-error arms" — the phase-gate wrapper went with
	// weather phases, James 2026-10-04⟧
	t.Run("two internal-error arms are structural only", func(t *testing.T) {
		t.Log("W5e-3: the two trailing `domain: dispatch: %w` arms (json.Marshal, store.Author) are internal-error wrappers with no caller-reachable trigger. They are pinned STRUCTURALLY by TestW5eDispatchRefusalSetUnchanged in internal/store, and are named here so a reader does not mistake their absence for an omission.")
	})
}

// TestW5eEveryCargoValueIsAccepted — W5e-3's own sentence: "A cargo-bearing
// and a cargo-less dispatch are both accepted." This is the assertion
// (m5e-3) — "an empty cargo note rejected, length capped" — dies on.
func TestW5eEveryCargoValueIsAccepted(t *testing.T) {
	cases := []struct {
		Name  string
		Value *string
	}{
		{"absent", nil},
		{"empty string", w5ePtr("")},
		{"whitespace only", w5ePtr("   \t\n")},
		{"the word empty", w5ePtr("empty")},
		{"the word none", w5ePtr("none")},
		{"ordinary note", w5ePtr("3 pallets drill pipe")},
		{"2 KB", w5ePtr(strings.Repeat("drill pipe, ", 170) + "end")},
		{"8 KB", w5ePtr(strings.Repeat("a", 8192))},
		{"newlines and tabs", w5ePtr("line one\nline two\tend")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			w := w5eNewWorld(t)
			// ⟦LOCK — §62, 2026-09-03⟧ R3, same remedy, same position: above the window.
			w5eSeedCaseVehicles(t, w)
			before := len(w.events(w.a, "site-a"))
			dest := "site-b"
			deadline := int64(60)
			trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
				// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §62 class R3⟧ NOT
				// RE-SEEDED. This case asserts `after != before+1` — "minted %d event(s), want
				// exactly 1". A vehicle record makes it two. PLAN OWNER's.
				//
				// ⟦LOCK — §62, 2026-09-03⟧ R3, PLACED ON ITS OWN EVIDENCE. ⚠ §62's R3 site list
				// names only `TestW5eEveryPreChunkDispatchRefusalStillFires`; this site is in a
				// SECOND test in the same file, so it was classified from the mechanism rather
				// than from the list — and the mechanism is identical: `before := len(w.events(...))`
				// captured, then an exact event count asserted against it. Same disposition, same
				// remedy, same seed call. The expected value `before+1` is UNTOUCHED.
				// ⟦was: "VehicleEntered:    \"TRUCK 12\","⟧
				VehicleID:         w7SeedVehicleID(t, w.a, "site-a", "TRUCK 12"),
				DriverPersonID:    w.driver.PersonID,
				OccupantPersonIDs: []string{w.passenger.PersonID},
				DestinationSite:   &dest,
				DeadlineMinutes:   &deadline,
				CargoNote:         tc.Value,
			}, 0, "default")
			if err != nil {
				t.Fatalf("W5e-3 (m5e-3): a departure carrying cargo note %s was REFUSED: %v.\nW5e adds a RECORDED FIELD, not a rule. Refusing to record a departure that is happening anyway leaves the highest-risk truck on the ice with no trip, no timer and no muster trail — the operator ruling this codebase already carries at tripauthor.go's PhaseGate comment. An empty note, a whitespace note and an 8 KB note are all lawful records of what the officer chose to write",
					w5eShow(tc.Value), err)
			}
			if after := len(w.events(w.a, "site-a")); after != before+1 {
				t.Fatalf("W5e-3: authoring a departure with cargo %s minted %d event(s), want exactly 1", w5eShow(tc.Value), after-before)
			}
			if trip.TripID == "" {
				t.Fatalf("W5e-3: Dispatch returned an empty trip id for cargo %s", w5eShow(tc.Value))
			}
		})
	}
}
