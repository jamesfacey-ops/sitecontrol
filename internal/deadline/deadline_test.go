// C7 done-when tests. Naming rule: each test name states what it PROVES and
// the body proves exactly that (the estate's verifier hunts for names wider
// than their bodies).
//
// Clock discipline in these tests: monotonic arithmetic is never faked (it
// cannot be); G is controlled by handing OnApplied a synthetic anchor — the
// hook's contract is "the pre-rotation anchor at delivery", so an anchor N
// minutes in the past IS a link that was down N minutes — plus the gap to
// the hook's own reading, so a G that is asserted as a whole-minute FIGURE
// must not sit on the rounding boundary (see the §9.3 test). The wall clock is
// faked only through Deadlines.WallNow, which the production code consults
// only on the reboot-bridge branch — exactly the branch that needs it.
package deadline

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

const (
	origin = "site-a" // the dispatching site (A)
	here   = "site-b" // the destination site whose scanner is under test (B)
	// ⟦LOCK — §65 B3/B4, 2026-09-03⟧ vehicleHome is the post whose stream carries the VEHICLE
	// RECORD, and it is deliberately NEITHER of the two above.
	//
	// ⚠ THE REASON IS A PRODUCED FAILURE, not a preference. `dispatchInbound`'s trip is the
	// FIRST event on `origin`'s stream, so a record seeded ahead of it takes `(site-a, 1, 1)`
	// and pushes the dispatch to seq 2 — and `w9c_order_test.go:70-73` STATES BY HAND that
	// "the dispatch owns (site-a, 1, 1), so a site-a ack or retraction takes seq 2", built on
	// this very helper via `w9cAlertingTrip` (w9c_rearm_test.go:28). `here` is no escape: the
	// same fixture pins `{site-b, 1, 1}` and `{site-b, 1, 2}`. BOTH pinned streams are full.
	//
	// ⭐ A THIRD STREAM IS FREE, and the record does not need to be on either: [MEASURED —
	// `tripauthor.go:444`] `vehicleNumberSnapshot` resolves the record BY ID, not by site. So
	// the number reaches the alert while every hand-stated key keeps its value.
	// [MEASURED] `Alert.Vehicle="TRUCK 12"`, dispatch key `(site-a,1,1)`, and the package's
	// failure set is IDENTICAL with and without this seed.
	//
	// ⚠⚠ THE PREFERRED OPTION WAS TRIED FIRST AND THE STORE REFUSED IT ⟦§71(c) option 1⟧.
	// "Same site, EARLIER epoch" — this truck was registered before the current boot — would
	// have introduced no new post at all. It is not available: [MEASURED, by authoring]
	//     domain.CreateVehicle(st, "site-a", 0, …)
	//       -> "domain: create vehicle: envelope (site-a, epoch 0, seq 1): node_epoch must be >= 1"
	// `envelope.go:111` floors the epoch at 1, and epoch 1 is ALREADY the pinned dispatch's
	// (`(site-a, 1, 1)`). ⭐ There is no epoch below the one the fixture already owns, so the
	// option is closed by the store's own validation rather than by preference.
	//
	// ⚠ WHAT THE FALLBACK COSTS, STATED SO A LATER READER MEETS A FIXTURE FACT AND NOT A PUZZLE
	// ⟦§71(c) option 2⟧: THIS FIXTURE DELIBERATELY REGISTERS THE TRUCK'S RECORD AT A POST THAT
	// APPEARS NOWHERE ELSE IN IT. That is not an oversight and not a stray site id. It is the
	// only remaining stream, and it is harmless HERE for a measured reason: nothing in this
	// package reads the vehicle's SiteID — the alert reads the NUMBER, through an id-keyed
	// lookup that does not consult the record's site at all.
	vehicleHome = "site-c"
)

func newSite(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	if err := Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

func newScanner(t *testing.T, st *store.Store) *Deadlines {
	t.Helper()
	d, err := New(st, here, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// dispatchInbound authors an A→B trip with deadline X directly into this
// store (the injected-event pattern every fold suite uses: Apply does not
// care whose site_id an envelope bears; transport identity is C3a's, already
// proven there) and returns the trip plus its exact envelope from the log.
func dispatchInbound(t *testing.T, st *store.Store, xMinutes int64, driver string) (domain.Trip, store.Envelope) {
	t.Helper()
	dest := here
	trip, err := domain.Dispatch(st, origin, 1, domain.DispatchInput{
		// ⚠⚠ ⟦ROUTED BACK — §62, 2026-09-03⟧ THIS FILE IS ONE OF THE FOUR §62 bar 3 LEAVES
		// UN-PRE-CLASSIFIED. On its own evidence the site is R3-SHAPED — an event authored
		// inside a window somebody else measures — ⚠ BUT R3's REMEDY IS PROVABLY UNAVAILABLE
		// HERE, which is a different thing from R3 being the wrong class.
		//
		// R3 says "seed BEFORE the window is captured". THERE IS NO BEFORE. [MEASURED] this
		// dispatch is the FIRST event on site-a's stream, so any `vehicle_created` seeded ahead
		// of it TAKES `(site-a, 1, 1)` and pushes the dispatch to seq 2 — which is exactly the
		// produced failure the note below records. And the keys are stated BY HAND in a second
		// file: `w9c_order_test.go:70-73` reads "The dispatch owns (site-a, 1, 1), so a site-a
		// ack or retraction takes seq 2", built on this very helper through `w9cAlertingTrip`
		// (`w9c_rearm_test.go:28`). ⚠ Seeding on the OTHER stream does not escape it either:
		// the same fixture pins `{site-b, 1, 1}` and `{site-b, 1, 2}`. Both streams are pinned.
		//
		// ⚠ SO THE TWO REMAINING REPAIRS ARE BOTH BARRED TO THIS PASS:
		//   (a) restate the W9c fixtures' pinned keys — an EXPECTATION CHANGE, which §47's
		//       ruling reserves to the plan owner in terms;
		//   (b) pass a bare id and seed no record — compiles, and every deadline test passes,
		//       ⛔ but `Alert.Vehicle` silently becomes "" on the overdue-alert surface
		//       (`scan.go:227` via `vehicleSnapshotText`, which maps a nil snapshot to ""), and
		//       [MEASURED] NO test in this package asserts `Alert.Vehicle`, so it would go
		//       green unseen. That is the green-going repair, not a repair.
		//
		// ⟦LOCK — §65 B4, 2026-09-03⟧ RE-SEEDED — see `vehicleHome` above for the stream choice
		// and the measurement behind it. The route below is DISCHARGED and PRESERVED VERBATIM
		// underneath rather than deleted: it is the record of why the obvious seed is wrong.
		// ⟦was: "VehicleEntered: \"TRUCK 12\", DriverPersonID: driver,"⟧
		//
		// ⚠ ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §65 B4⟧ NOT RE-SEEDED, AND THE
		// REASON IS A PRODUCED FAILURE, NOT A JUDGEMENT CALL. Re-seeding this
		// line by id was applied, run, and REVERTED: creating the vehicle
		// record authors a `vehicle_created` event AHEAD of the trip on
		// site-a's stream, so this dispatch's envelope moves from
		// (site-a, epoch 1, seq 1) to seq 2. THREE W9c TESTS PIN THAT KEY BY
		// HAND — w9c_order_test.go's "The dispatch owns (site-a, 1, 1)" key
		// arithmetic and w9c_rearm_test.go's "STATED fixture keys — never
		// inherited" precondition — and all three went RED. Repairing them
		// means restating those fixtures' keys, which is an expectation
		// change and the PLAN OWNER's. See reports/window-build/RESEED-164.md §3.
		VehicleID: w7SeedVehicleID(t, st, vehicleHome, "TRUCK 12"), DriverPersonID: driver,
		DestinationSite: &dest, DeadlineMinutes: &xMinutes,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events(origin, 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v (%d)", err, len(envs))
	}
	return trip, envs[0]
}

func findAlert(t *testing.T, b Board, tripID string) Alert {
	t.Helper()
	for _, a := range b.Alerts {
		if a.TripID == tripID {
			return a
		}
	}
	t.Fatalf("trip %s not on the board (%d rows)", tripID, len(b.Alerts))
	return Alert{}
}

func mustEval(t *testing.T, d *Deadlines) Board {
	t.Helper()
	b, err := d.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// --- THE test (§9.3's under-alert hole; the plan's single most important
// negative case). A trip whose deadline expired while the link was down must
// sit in overdue-unresolved after the heal — never a live alert, never a
// fresh countdown, not even after re-sighting, rescanning, or a process
// restart. Failability was proven by mutation (see build-c7.md): with the
// expired-at-receipt branch replaced by the naive "start X from local first
// sight" rule, this test fails on the first Status assert.
func TestPartitionExpiredTripNeverEarnsFreshCountdownAfterHeal(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// X = 2 min, and the envelope arrives bearing an anchor 2½ min old: the
	// link was down longer than the whole deadline. This is the post-heal
	// catch-up delivery, exactly as the pull loop would hand it to the hook.
	//
	// Why 2½ and not a round 3: the hook takes its OWN time.Now() as the
	// receipt a moment after this one, so G is the anchor's age PLUS that
	// gap, and the radio framing rounds G UP to whole minutes (scan.go,
	// `(g_ms + 59_999) / 60_000`). An anchor of exactly 3 min therefore sat
	// ON the rounding boundary of the very figure asserted below: one
	// millisecond of gap made it read "4 min". [MEASURED 2026-09-09] under
	// CPU oversubscription 16/2000 captures crossed that millisecond; 0/1000
	// unloaded — hence green in isolation, red under the whole suite. 2½ min
	// is mid-bucket: it still rounds up to 3 unless the gap reaches 30 s.
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Now().Add(-(2*time.Minute + 30*time.Second)), true)

	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("partition-expired trip has status %q, want %q — a live alert or countdown here IS the §9.3 under-alert hole", a.Status, StatusOverdueUnresolved)
	}
	if a.Remaining != 0 {
		t.Fatalf("partition-expired trip shows a remaining countdown of %s — it must have none", a.Remaining)
	}
	if !strings.Contains(a.Reason, "link was down 3 min") {
		t.Fatalf("review framing = %q, want the link-was-down-N-min radio framing", a.Reason)
	}

	// Re-sighting must never re-anchor (row 29's side door): even a
	// (buggy or replayed) second delivery carrying a FRESH anchor changes
	// nothing — the capture is insert-once on the source key.
	d.OnApplied(env, time.Now(), true)
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueUnresolved {
		t.Fatalf("after a re-sighting with a fresh anchor: status %q — the capture re-anchored", a.Status)
	}

	// No post-heal storm: repeated scans are stable.
	for i := 0; i < 3; i++ {
		if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueUnresolved {
			t.Fatalf("scan %d flipped the trip to %q", i, a.Status)
		}
	}

	// And a process restart must not resurrect it either: a fresh scanner
	// over the same database (no in-process receipts) still refuses.
	d2 := newScanner(t, st)
	if a := findAlert(t, mustEval(t, d2), trip.TripID); a.Status != StatusOverdueUnresolved {
		t.Fatalf("after simulated process restart: status %q, want overdue-unresolved", a.Status)
	}
}

// --- Ruling 3 (row 15), verbatim requirement: an open inbound trip first
// sighted with NO persisted capture is overdue-unresolved — never a live
// countdown from the current Anchor(). Failability proven by mutation (see
// build-c7.md): with the no-capture branch replaced by "capture now at full
// X", this test fails on the Status assert.
func TestOpenInboundTripWithNoPersistedCaptureIsOverdueUnresolvedNeverACountdown(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, _ := dispatchInbound(t, st, 2, "driver-1") // applied, but the hook never saw it
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("capture-less trip has status %q, want overdue-unresolved", a.Status)
	}
	if !strings.Contains(a.Reason, "no receipt record") {
		t.Fatalf("reason = %q", a.Reason)
	}
}

// A capture taken before the box held a §9.3 anchor (first drain after a
// restart, or a from-zero replay) records G as unknown — review lane.
func TestCaptureTakenWithoutAnchorIsOverdueUnresolved(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Time{}, false)
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("anchorless capture has status %q, want overdue-unresolved", a.Status)
	}
	if !strings.Contains(a.Reason, "sync anchor") {
		t.Fatalf("reason = %q", a.Reason)
	}
}

// The routine path: a healthy capture counts down on the monotonic clock and
// expires into a LIVE overdue alert (this is the demo's "banner at ~X" state,
// compressed: remaining-at-receipt is made tiny by handing the hook an anchor
// just short of X, then real monotonic time runs it out).
func TestHealthyCountdownExpiresIntoLiveOverdueAlertOnTheMonotonicClock(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+80*time.Millisecond), true) // X−G = 80ms

	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusCountdown {
		t.Fatalf("before expiry: status %q, want countdown", a.Status)
	}
	time.Sleep(150 * time.Millisecond)
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueLive {
		t.Fatalf("after expiry: status %q, want overdue_live", a.Status)
	}
	if a.OverdueBy <= 0 {
		t.Fatalf("overdue-by = %s, want > 0", a.OverdueBy)
	}
	// ⟦LOCK — §65 B3 condition (ii), 2026-09-03⟧ ⭐ THE ALERT NAMES THE TRUCK, BY ITS NUMBER.
	//
	// ⚠⚠ THIS ASSERTION EXISTS BECAUSE ITS ABSENCE WAS THE HOLE. Nothing in this package
	// asserted `Alert.Vehicle` — [MEASURED, `grep -n '\.Vehicle\b' internal/deadline/*_test.go`
	// returned NOTHING before this line — and that silence is exactly what would have let the
	// bare-id repair through: passing a vehicle id that names no record leaves
	// `vehicleNumberSnapshot` returning NIL, `vehicleSnapshotText` mapping that to "", and the
	// overdue alert naming NO TRUCK AT ALL — while every test in this package stayed green.
	// A green reached that way is indistinguishable from a real pass.
	//
	// ⚠ It is asserted on the DERIVED NUMBER and never on the record id. `scan.go:43` says
	// "verbatim entered form, always (M8)" and `tripauthor.go:28` says `trip.vehicle_entered` is
	// "DERIVED from the composed record's own number" — so an alert that read the id would be
	// showing the operator `site-c-vehicle-<hex>` at the moment a truck is overdue.
	if a.Vehicle != "TRUCK 12" {
		t.Fatalf("the overdue alert names the vehicle as %q, want %q — an alert that cannot say WHICH TRUCK "+
			"is overdue is the one thing this board exists to say. An empty value here means the trip's "+
			"vehicle record did not resolve (vehicleNumberSnapshot returned nil), which is what a fixture "+
			"that passes an id naming no record produces — silently, and green", a.Vehicle, "TRUCK 12")
	}
}

// §9.3 rule 3 / A1d: a live alert raised while current staleness to the
// origin exceeds the sync interval carries "last heard from <origin>: N" on
// its face; a current link carries nothing.
func TestLiveAlertRaisedWhileStaleCarriesLastHeardFromOriginOnItsFace(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+30*time.Millisecond), true)
	time.Sleep(60 * time.Millisecond)

	d.Staleness = func(o string) (time.Duration, bool) { return 4 * time.Hour, true }
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusOverdueLive {
		t.Fatalf("status %q, want overdue_live", a.Status)
	}
	// Shared phrasing (internal/humantime), matching design §8.9's own
	// example wording — was raw Duration.String() ("4h0m0s") before the
	// verifier's fix-1 (2026-08-19) unified it with C10's sync-status ages.
	// ⟦was: "last heard from site-a: 4 hours ago" — FIX5: capitalised, its own sentence⟧
	if !strings.Contains(a.StalenessFace, "Last heard from site-a: 4 hours ago") {
		t.Fatalf("staleness face = %q, want it to carry the 4-hour staleness in the shared phrasing", a.StalenessFace)
	}

	d.Staleness = func(o string) (time.Duration, bool) { return 2 * time.Second, true }
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.StalenessFace != "" {
		t.Fatalf("current link (2s < 10s interval) must carry no staleness face, got %q", a.StalenessFace)
	}

	d.Staleness = func(o string) (time.Duration, bool) { return 0, false }
	if a := findAlert(t, mustEval(t, d), trip.TripID); !strings.Contains(a.StalenessFace, "never") {
		t.Fatalf("never-heard origin must read 'never' on the face, got %q", a.StalenessFace)
	}
}

// The reboot bridge, local process-restart half: a fresh scanner over the
// same database (in-process monotonic receipts gone) computes elapsed from
// the persisted receipt wall-time — mid-countdown it keeps counting, past
// the deadline the alert still fires. (The container-level pct stop/start
// rehearsal on the C4 environment is explicitly out of this chunk's hands.)
func TestAlertStillFiresAfterProcessRestartViaThePersistedWallBridge(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Now().Add(-30*time.Second), true) // remaining-at-receipt = 90s
	receiptWall := time.Now()

	restarted := newScanner(t, st) // fresh instance, same DB: the restart
	restarted.WallNow = func() time.Time { return receiptWall.Add(30 * time.Second) }
	a := findAlert(t, mustEval(t, restarted), trip.TripID)
	if a.Status != StatusCountdown {
		t.Fatalf("mid-countdown after restart: status %q, want countdown (bridge must keep counting, not reset)", a.Status)
	}
	if a.Remaining > 90*time.Second || a.Remaining < 30*time.Second {
		t.Fatalf("remaining after bridge = %s, want ~60s — a value near the full 2m would mean the bridge granted a fresh countdown", a.Remaining)
	}

	restarted.WallNow = func() time.Time { return receiptWall.Add(5 * time.Minute) }
	if a := findAlert(t, mustEval(t, restarted), trip.TripID); a.Status != StatusOverdueLive {
		t.Fatalf("past deadline after restart: status %q, want overdue_live — the process-restart path must still fire", a.Status)
	}
}

// The bridge's failure modes (§9.4): a backwards wall clock across a restart
// — and a known-bad one — drop the trip to overdue-unresolved, erring early.
func TestBackwardsOrKnownBadWallClockAcrossRestartDropsTripToOverdueUnresolved(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Now().Add(-10*time.Second), true) // healthy capture

	restarted := newScanner(t, st)
	restarted.WallNow = func() time.Time { return time.Now().Add(-10 * time.Minute) } // backwards
	a := findAlert(t, mustEval(t, restarted), trip.TripID)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("backwards clock: status %q, want overdue-unresolved", a.Status)
	}
	if !strings.Contains(a.Reason, "backwards") {
		t.Fatalf("reason = %q", a.Reason)
	}

	restarted2 := newScanner(t, st)
	restarted2.WallNow = func() time.Time { return time.Unix(60, 0) } // dead RTC
	if a := findAlert(t, mustEval(t, restarted2), trip.TripID); a.Status != StatusOverdueUnresolved {
		t.Fatalf("known-bad clock: status %q, want overdue-unresolved", a.Status)
	}
}

// M2c, the replay-gate unit test the chunk names: a scan run while a
// from-zero replay is in progress puts an open trip expectation in
// overdue-unresolved and leaves a degenerate trip untouched. The gate is a
// gate, not a latch: lowering it restores the healthy countdown.
func TestScanDuringFromZeroReplayMarksOpenExpectationOverdueUnresolvedAndLeavesDegenerateTripUntouched(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// A healthy expectation that would otherwise be a live countdown…
	trip, env := dispatchInbound(t, st, 60, "driver-1")
	d.OnApplied(env, time.Now().Add(-5*time.Second), true)
	// …and a degenerate local trip (no destination, no deadline, §10),
	// whose envelope the hook also sees — as it would in a real replay.
	deg, err := domain.DispatchLocalCheckIn(st, here, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: "driver-2","⟧
		VehicleID: w7SeedVehicleID(t, st, here, "gate walk-in"), DriverPersonID: "driver-2",
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	degEnvs, err := st.Events(here, 1, deg.Seq, 1)
	if err != nil || len(degEnvs) != 1 {
		t.Fatalf("fetch degenerate envelope: %v", err)
	}
	d.OnApplied(degEnvs[0], time.Now(), true)

	d.SetReplaying(true)
	board := mustEval(t, d)
	a := findAlert(t, board, trip.TripID)
	if a.Status != StatusOverdueUnresolved {
		t.Fatalf("during replay: status %q, want overdue-unresolved", a.Status)
	}
	if !strings.Contains(a.Reason, "replay") {
		t.Fatalf("reason = %q", a.Reason)
	}
	// Degenerate trip untouched: not on the board, and no capture row was
	// ever written for it.
	for _, row := range board.Alerts {
		if row.TripID == deg.TripID {
			t.Fatalf("degenerate trip %s appeared on the deadline board", deg.TripID)
		}
	}
	caps, err := d.loadCaptures()
	if err != nil {
		t.Fatal(err)
	}
	for k, c := range caps {
		if c.tripID == deg.TripID {
			t.Fatalf("degenerate trip %s has a capture row %v", deg.TripID, k)
		}
	}

	d.SetReplaying(false)
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusCountdown {
		t.Fatalf("after replay ends: status %q, want the healthy countdown back", a.Status)
	}
}

// The capture hook's selection rule: only inbound deadline trips for THIS
// site produce capture rows — degenerate trips, foreign destinations and
// non-trip events never enter C7 state.
func TestCaptureIgnoresDegenerateForeignDestinationAndNonTripEnvelopes(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)

	// Inbound deadline trip → captured.
	trip, env := dispatchInbound(t, st, 5, "driver-1")
	d.OnApplied(env, time.Now(), true)

	// Degenerate local trip → ignored.
	// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "deg, err := domain.DispatchLocalCheckIn(st, here, 1, domain.DispatchInput{VehicleEntered: "TRK-14", DriverPersonID: "d2"}, 0, "default")"⟧
	deg, err := domain.DispatchLocalCheckIn(st, here, 1, domain.DispatchInput{VehicleID: w7SeedVehicleID(t, st, here, "TRK-14"), DriverPersonID: "d2"}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	degEnv, _ := st.Events(here, 1, deg.Seq, 1)
	d.OnApplied(degEnv[0], time.Now(), true)

	// Deadline trip destined for a THIRD site → ignored here.
	other := "site-c"
	x := int64(5)
	foreign, err := domain.Dispatch(st, origin, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 9", DriverPersonID: "d3", DestinationSite: &other, DeadlineMinutes: &x,"⟧
		VehicleID: w7SeedVehicleID(t, st, origin, "TRUCK 9"), DriverPersonID: "d3", DestinationSite: &other, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fEnv, _ := st.Events(origin, 1, foreign.Seq, 1)
	d.OnApplied(fEnv[0], time.Now(), true)

	// A non-trip event → ignored.
	if _, err := domain.CreateCompany(st, here, 1, "Kakivik", 0, "default"); err != nil {
		t.Fatal(err)
	}
	coEnvs, err := st.Events(here, 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	d.OnApplied(coEnvs[len(coEnvs)-1], time.Now(), true)

	caps, err := d.loadCaptures()
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 {
		t.Fatalf("capture rows = %d, want exactly 1 (the inbound deadline trip)", len(caps))
	}
	for _, c := range caps {
		if c.tripID != trip.TripID {
			t.Fatalf("captured trip = %s, want %s", c.tripID, trip.TripID)
		}
	}
}

// §9.4/A14: an explicit acknowledgment is an authored event — it lands in
// the event log, folds into alert_ack, and dismisses the banner. The board
// row survives (the trip is still open); only the banner is dismissed.
func TestExplicitAcknowledgmentDismissesBannerAndLandsInEventLog(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 2, "driver-1")
	d.OnApplied(env, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // G = 2m30s: still overdue-unresolved, off the whole-minute rounding boundary

	if banner := mustEval(t, d).Banner(); len(banner) != 1 {
		t.Fatalf("banner rows before ack = %d, want 1", len(banner))
	}

	wmBefore, err := st.Watermark(here, 1)
	if err != nil {
		t.Fatal(err)
	}
	post := "Ice Road"
	if err := Acknowledge(st, here, 1, trip.TripID, &post, 0, "default"); err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events(here, 1, wmBefore+1, 0)
	if err != nil || len(envs) != 1 {
		t.Fatalf("expected exactly one new authored event, got %d (%v)", len(envs), err)
	}
	if !strings.Contains(string(envs[0].Payload), `"alert_acknowledged"`) {
		t.Fatalf("logged event payload = %s", envs[0].Payload)
	}

	board := mustEval(t, d)
	if banner := board.Banner(); len(banner) != 0 {
		t.Fatalf("banner rows after ack = %d, want 0", len(banner))
	}
	if a := findAlert(t, board, trip.TripID); !a.Acked {
		t.Fatal("board row must still exist, marked acknowledged")
	}

	// Acknowledging a trip this site has never heard of is refused and
	// authors nothing.
	if err := Acknowledge(st, here, 1, "no-such-trip", nil, 0, "default"); err == nil {
		t.Fatal("acknowledging an unknown trip must refuse")
	}
	if wm, _ := st.Watermark(here, 1); wm != wmBefore+1 {
		t.Fatalf("refusal authored an event (watermark %d → %d)", wmBefore+1, wm)
	}
}

// An arrival check-in closes the trip fold's open expectation, which removes
// it from the scan entirely — the banner clears through the ordinary event
// machinery, no C7 state edit involved.
func TestArrivalCheckInClearsTheBanner(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	// The arrival authoring path builds visits from the roster, so the
	// driver must be a real person here.
	p, err := domain.CreatePerson(st, here, 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, env := dispatchInbound(t, st, 2, p.PersonID)
	d.OnApplied(env, time.Now().Add(-(2*time.Minute+30*time.Second)), true) // off the whole-minute rounding boundary (see fix/deadline-flake)
	if banner := mustEval(t, d).Banner(); len(banner) != 1 {
		t.Fatalf("banner rows = %d, want 1", len(banner))
	}
	if _, err := domain.RecordExpectedArrival(st, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	board := mustEval(t, d)
	if banner := board.Banner(); len(banner) != 0 {
		t.Fatalf("banner rows after arrival = %d, want 0", len(banner))
	}
	for _, a := range board.Alerts {
		if a.TripID == trip.TripID {
			t.Fatalf("arrived trip still on the deadline board as %q", a.Status)
		}
	}
}

// End-to-end through the REAL pull path: engine B pulls engine A over HTTP
// with the hook wired exactly as main.go wires it; the third drain delivers
// a fresh dispatch with a current pre-rotation anchor, and the scan shows a
// healthy countdown near the full X. This proves the composition, not the
// arithmetic (the arithmetic tests above control G synthetically).
func TestCaptureThroughRealPullPathYieldsHealthyCountdown(t *testing.T) {
	ctx := context.Background()

	stA, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stA.Close() })
	if err := domain.Register(stA); err != nil {
		t.Fatal(err)
	}
	if err := Register(stA); err != nil {
		t.Fatal(err)
	}
	cfgA := &config.Config{SiteID: origin, ListenAddr: ":0", NodeEpoch: 1,
		DBPath: stA.Path, SyncIntervalSeconds: 10, PeerKey: "k"}
	engA := repl.New(cfgA, stA)
	srvA := httptest.NewServer(engA.SyncHandler())
	t.Cleanup(srvA.Close)

	stB := newSite(t)
	cfgB := &config.Config{SiteID: here, ListenAddr: ":0", NodeEpoch: 1,
		DBPath: stB.Path, SyncIntervalSeconds: 10, PeerKey: "k",
		Peers: []config.Peer{{SiteID: origin, BaseURL: srvA.URL, Key: "k"}}}
	engB := repl.New(cfgB, stB)
	d := newScanner(t, stB)
	engB.OnApplied = d.OnApplied // the main.go wiring, verbatim

	// Two drains establish the §9.3 anchor (Anchor() exposes the previous
	// drain instant); the trip then arrives on the third.
	if err := engB.PullPeer(ctx, origin); err != nil {
		t.Fatal(err)
	}
	if err := engB.PullPeer(ctx, origin); err != nil {
		t.Fatal(err)
	}
	trip, _ := func() (domain.Trip, store.Envelope) {
		dest := here
		x := int64(60)
		tr, err := domain.Dispatch(stA, origin, 1, domain.DispatchInput{
			// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRUCK 12", DriverPersonID: "driver-1","⟧
			VehicleID: w7SeedVehicleID(t, stA, origin, "TRUCK 12"), DriverPersonID: "driver-1",
			DestinationSite: &dest, DeadlineMinutes: &x,
		}, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return tr, store.Envelope{}
	}()
	if err := engB.PullPeer(ctx, origin); err != nil {
		t.Fatal(err)
	}

	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusCountdown {
		t.Fatalf("through the real pull path: status %q (reason %q), want countdown", a.Status, a.Reason)
	}
	if a.Remaining < 59*time.Minute {
		t.Fatalf("remaining = %s, want ≈60m (G on the routine path is near zero)", a.Remaining)
	}
}
