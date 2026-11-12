// w9a_consumers_test.go — W9a's rendered-surface criteria:
//
//	W9a-2  (T2) the composed value at EACH of §9.2's ELEVEN consumers
//	W9a-14 (T14) the OVERDUE BANNER composes its Class-A fields (CRITICAL)
//	W9a-15 (T15) the Class-B amendment that OUTLIVES ITS GATE
//	W9a-1  (T1) render half — the incident read shows the amended driver
//	W9a-3  (T3) render half — the rendered muster page is byte-identical
//	W9a-7  (T7b) the ALERT STAYS LIVE across an arrival-claim correction
//	W9a-16 (T16)(a) the scoped-read probe for the ARRIVAL reader
//
// See internal/domain/w9a_seams_test.go for the one design decision every
// W9a file rests on.
package web

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// =========================================================================
// W9a-2 (T2) — the ELEVEN consumers
// =========================================================================

// w9aConsumer is one row of F4 §9.2's NUMBERED list. The list is numbered
// because the criterion locks a QUANTIFIER over it, and "a quantifier locked
// against an incomplete or uncountable list makes the incompleteness
// permanent and green".
type w9aConsumer struct {
	N      int
	Name   string
	Reads  string // F4 §9.2's "What composes" cell, abridged
	Probe  func(t *testing.T, fx *w9aWorld) (region string, err error)
	Raw    string // the marker the surface carries BEFORE the amendment
	Amend  string // the marker it must carry AFTER
	Shared string // non-empty when this probe's region overlaps another row's
	// ControlDeferred, when non-empty, is the WRITTEN REASON why this row's
	// raw-marker control cannot be established on the pre-chunk tree: the
	// surface does not render this fact AT ALL yet, and rendering it is part
	// of what the criterion orders. A deferred control is REPORTED on every
	// run, never silently skipped, and the row's real assertion still runs.
	ControlDeferred string
}

// w9aWorld is the fixture: one store, two posts, one trip per consumer.
type w9aWorld struct {
	st      *store.Store
	origin  *Server // site-a, "Ice Road"
	dest    *Server // site-b, "Pad Site Control"
	alerts  *deadline.Deadlines
	tripFor map[int]domain.Trip
	arrival domain.Arrival
}

// TestW9a2TheComposedValueIsProducedAtEachOfTheElevenConsumers — W9a-2.
//
// KILLER MUTANT: (m2) the SURVIVING RAW READER — "NotTracked (or ANY §9
// section) still reads bare `trip_occupant`". The quantifier is the whole
// point: a build that migrates ten consumers and misses one is green on any
// suite that watches only the three the scans happen to reach.
//
// ⚠ EVERY ROW IS CONTROLLED, BEFORE the amendment: the row's own region must
// carry the RAW marker. A row whose control fails is reported as a PROBE
// DEFECT naming the row — never as coverage, and never as a criterion red.
// That is the difference between "this consumer does not compose" and "this
// test cannot see this consumer".
//
// ⚠ NEITHER SOURCE SCAN SUBSTITUTES FOR THIS. F4 §9.3 says so in terms: the
// outward scan reds raw reads from OUTSIDE internal/domain; the inverse scan
// (W9a-8) reds effective reads INSIDE it but only in three recompute paths —
// so a raw `trip_occupant` read living inside internal/domain and outside
// those three paths is invisible to both BY CONSTRUCTION, and consumers 3, 6,
// 7 and 8 sit exactly there.
func TestW9a2TheComposedValueIsProducedAtEachOfTheElevenConsumers(t *testing.T) {
	fx := w9aBuildWorld(t)
	consumers := w9aConsumers()
	if len(consumers) != 11 {
		t.Fatalf("W9a-2 FIXTURE DEFECT: the consumer table holds %d rows, want ELEVEN. F4 §9.2 is numbered precisely so this can be checked "+
			"rather than believed; a quantifier locked against an uncountable list makes the incompleteness permanent and green", len(consumers))
	}

	// --- CONTROL PASS: every region carries the RAW value ---------------
	var defects []string
	for _, c := range consumers {
		region, err := c.Probe(t, fx)
		if err != nil {
			defects = append(defects, fmt.Sprintf("%d (%s): %v", c.N, c.Name, err))
			continue
		}
		if strings.Contains(region, c.Raw) {
			continue
		}
		if c.ControlDeferred != "" {
			t.Logf("W9a-2 CONTROL DEFERRED, consumer %d (%s): %s", c.N, c.Name, c.ControlDeferred)
			continue
		}
		defects = append(defects, fmt.Sprintf("%d (%s): region carries no RAW marker %q; F4 §9.2 says it reads %s", c.N, c.Name, c.Raw, c.Reads))
	}
	if len(defects) != 0 {
		t.Fatalf("W9a-2 PROBE DEFECT — %d of ELEVEN probes cannot tell a composed read from a raw one, so their verdicts below would prove nothing:\n  %s\n"+
			"⚠ THIS IS NOT A CRITERION RED. It says the PROBE needs re-binding (or the surface moved), not that the consumer fails to compose. "+
			"A gate must not book it either way", len(defects), strings.Join(defects, "\n  "))
	}

	// --- the amendments ------------------------------------------------
	w9aAmendTheWorld(t, fx)

	// --- the judgement, ELEVEN times ------------------------------------
	var missed []string
	for _, c := range consumers {
		region, err := c.Probe(t, fx)
		if err != nil {
			t.Fatalf("W9a-2 PROBE DEFECT, consumer %d (%s), after the amendment: %v", c.N, c.Name, err)
		}
		if !strings.Contains(region, c.Amend) {
			missed = append(missed, fmt.Sprintf("%d (%s): reads %s — no %q%s", c.N, c.Name, c.Reads, c.Amend, w9aSharedNote(c)))
		}
	}
	if len(missed) != 0 {
		t.Fatalf("W9a-2 (m2 — the surviving raw reader): %d of ELEVEN §9.2 consumers do NOT produce the composed value:\n  %s\n"+
			"The composed value must be produced at EACH of the eleven, not once. A build that migrates the rest and misses one of these is green "+
			"on every suite that watches only the consumers the two source scans happen to reach", len(missed), strings.Join(missed, "\n  "))
	}
}

func w9aSharedNote(c w9aConsumer) string {
	if c.Shared == "" {
		return ""
	}
	return " [shared region: " + c.Shared + "]"
}

// TestW9a2BothDirectionsAtTheNotTrackedSection — W9a-2's OTHER half, T2's
// own: "a removed occupant leaves NotTracked; an added unsighted occupant
// APPEARS in it".
//
// The add half kills the variant that composes removals but not additions —
// and it is the SAFE direction of [[D-15a]]: the correction SURFACES a
// previously invisible unaccounted person.
func TestW9a2BothDirectionsAtTheNotTrackedSection(t *testing.T) {
	st := w3OpenRegistered(t, "w9a-2-directions.db")
	w9aSeedPerson(t, st, "site-r", 1, "p-drv2", "Dana Driver")
	w9aSeedPerson(t, st, "site-r", 1, "p-leaves", "Leah Leaves")
	w9aSeedPerson(t, st, "site-r", 1, "p-appears", "Ada Appears")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-2D", "p-drv2", []string{"p-leaves"}, 240)
	if _, err := domain.RecordExpectedArrivalExcluding(st, "site-b", 1, trip.TripID,
		[]string{"p-leaves"}, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: %v", err)
	}

	before := w9aNotTrackedNames(t, st, trip.TripID)
	if !w9aHas(before, "Leah Leaves") {
		t.Fatalf("PRECONDITION (W9a-2): NotTracked does not list the unsighted occupant before any amendment — the fixture poses no case. Got %v", before)
	}
	if w9aHas(before, "Ada Appears") {
		t.Fatalf("PRECONDITION (W9a-2): NotTracked already lists the person the ADD half must make appear. Got %v", before)
	}

	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, OccupantsRemove: []string{"p-leaves"}, OccupantsAdd: []string{"p-appears"},
	}.payload())

	after := w9aNotTrackedNames(t, st, trip.TripID)
	// REMOVE direction. James, 2026-10-04: people in raw∖effective are listed in NotTracked from
	// the moment the correction is recorded (FINDINGS-R4 D1 ruling) ⟦was: "a person removed by
	// correction leaves the section — the record now says they were never out there" (F4 §7);
	// that left a removed, unsighted person on NO sheet⟧. The raw reader m2 hunts is still
	// detectable: a removed occupant must NOT be listed as "not aboard at arrival" (a composed
	// read puts them only in a TakenOffByCorrection row).
	rows, err := domain.NotTracked(st)
	if err != nil {
		t.Fatal(err)
	}
	var asStayed, asTakenOff bool
	for _, r := range rows {
		if r.TripID != trip.TripID {
			continue
		}
		for _, p := range r.Persons {
			if p.PersonID == "p-leaves" {
				if r.TakenOffByCorrection {
					asTakenOff = true
				} else {
					asStayed = true
				}
			}
		}
	}
	if asStayed {
		t.Fatalf("W9a-2 (m2 — the surviving raw reader), REMOVE direction: NotTracked still lists a removed occupant as NOT ABOARD AT ARRIVAL. Got %v", after)
	}
	if !asTakenOff {
		t.Fatalf("W9a-2, REMOVE direction (James, 2026-10-04 D1 ruling): a removed, unsighted occupant is on no NotTracked row — the person-on-no-list hole. Got %v", after)
	}
	if !w9aHas(after, "Ada Appears") {
		t.Fatalf("W9a-2, ADD direction: NotTracked does NOT list an occupant ADDED by amendment who has no visit and no sighting. Got %v.\n"+
			"This half kills the variant that composes removals but not additions, and it is the SAFE direction: the correction SURFACES a "+
			"previously invisible unaccounted person ([[D-15a]] on its own subject)", after)
	}
}

// =========================================================================
// W9a-14 (T14) — the overdue banner composes its Class-A fields (CRITICAL)
// =========================================================================

// TestW9a14TheOverdueBannerComposesItsClassAFields — W9a-14.
//
// KILLER MUTANT: (m17) the RAW BANNER — the build that migrates every
// surface EXCEPT the one an emergency is run from.
//
// LIVE CONSEQUENCE, in the criterion's own words: the officer reads the
// wrong number of people onto the radio when mounting a response, and the
// mirror sends responders after someone who never left.
//
// ⚠ THE BYTE-IDENTITY HALF IS NOT DECORATION: it kills the WRONG REPAIR —
// "fixing" the banner by feeding effective values into `computeStates`,
// which would redden W9a-7 and the inverse scan.
func TestW9a14TheOverdueBannerComposesItsClassAFields(t *testing.T) {
	srv, st, trip := w9aOverdueAtDestination(t)
	// Three aboard, driver Smith — the criterion's fixture.
	w9aSeedPerson(t, st, "site-r", 1, "p-smith", "Smith")
	w9aSeedPerson(t, st, "site-r", 1, "p-jones", "Jones")
	w9aSeedPerson(t, st, "site-r", 1, "p-a14", "Aida Fourteen")
	w9aSeedPerson(t, st, "site-r", 1, "p-b14", "Bo Fourteen")
	w9aSeedPerson(t, st, "site-r", 1, "p-d14", "Dee Fourteen")
	x := w9aDispatchOpenOverdue(t, st, srv, "TRK-14X", "p-smith", []string{"p-a14", "p-b14"})
	_ = trip

	// --- CONTROL: the banner renders the RAW values first ---------------
	banner := w9aBannerRegion(t, srv)
	if !strings.Contains(banner, "Smith") || !strings.Contains(banner, "3 aboard") {
		t.Fatalf("W9a-14 PROBE DEFECT: the overdue banner does not read \"3 aboard (driver Smith)\" before any amendment, so it cannot show that it "+
			"composed afterwards. Banner region:\n%s", w3PageText(banner))
	}
	stateBefore := w9aProjection(t, st, "trip_state")
	arrStateBefore := w9aProjection(t, st, "arrival_state")

	// --- the amendment §4 PERMITS at this moment (Class A, open trip) ---
	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: x.TripID, OccupantsAdd: []string{"p-d14"}, Driver: w9aStr("p-jones"),
	}.payload())

	banner = w9aBannerRegion(t, srv)
	if !strings.Contains(banner, "4 aboard") {
		t.Fatalf("W9a-14 (m17 — the RAW BANNER): after `trip_amended{occupants_add:[Dee], driver: Jones}` the overdue banner does not read \"4 aboard\". "+
			"§4 PERMITS this amendment at this moment — X is open and these are Class-A facts — and F4 §4 says composing them on the banner is "+
			"\"not merely permitted; it is required\". THE OFFICER READS THIS NUMBER ONTO THE RADIO. Banner region:\n%s", w3PageText(banner))
	}
	if !strings.Contains(banner, "Jones") {
		t.Fatalf("W9a-14 (m17): the overdue banner does not name the amended driver Jones. Banner region:\n%s", w3PageText(banner))
	}

	// --- the MIRROR half: a removal makes the count FALL ----------------
	w9aApplyNext(t, st, "site-b", 1, w2Base+2*w9aMinute, w9aTripAmend{
		TripID: x.TripID, OccupantsRemove: []string{"p-a14"},
	}.payload())
	if banner = w9aBannerRegion(t, srv); !strings.Contains(banner, "3 aboard") {
		t.Fatalf("W9a-14 (m17), MIRROR half: after removing an occupant the banner's count did not FALL back to \"3 aboard\". "+
			"The mirror sends responders after someone who never left. Banner region:\n%s", w3PageText(banner))
	}

	// ⟦was: the OverrideFace half — an amended `phase_override_reason` had to
	// reach the banner's override face. The fact and the face went with
	// weather phases (James, 2026-10-04); the driver and occupant halves
	// above stand.⟧

	// --- and derived state NEVER moved (I-2) ----------------------------
	if got := w9aProjection(t, st, "trip_state"); got != stateBefore {
		t.Fatalf("W9a-14: `trip_state` MOVED across the amendments. That is the WRONG REPAIR — feeding effective values into `computeStates` — "+
			"and it would redden W9a-7 and the §9.3 inverse scan.\nbefore: %s\nafter:  %s", stateBefore, got)
	}
	if got := w9aProjection(t, st, "arrival_state"); got != arrStateBefore {
		t.Fatalf("W9a-14: `arrival_state` MOVED across the amendments — the wrong repair again.\nbefore: %s\nafter:  %s", arrStateBefore, got)
	}
}

// =========================================================================
// W9a-15 (T15) — the Class-B amendment that outlives its gate
// =========================================================================

// TestW9a15TheClassBAmendmentThatOutlivesItsGate — W9a-15.
//
// KILLER MUTANT: (m18) the STALE-SELECTION SCANNER — the build that migrates
// the DISPLAY fields and leaves deadline and destination raw in
// `OpenExpectations`. It is green on every fixture that never reopens a
// Class-B-amended trip.
//
// LIVE CONSEQUENCE: the board promises 90 minutes and `Banner()` will not
// raise the trip for 240 — two and a half hours in which every trip surface
// says the vehicle is overdue and NO ALERT ROW EXISTS ON ANY SCREEN.
//
// ⚠⚠ THIS CRITERION DEPENDS ON W9b. Its fixture authors `POST /arrivals/unref`,
// which is W9b's surface: the world it pins — a Class-B amendment legally
// authored on a trip that `arrival_ref_retracted` then reopens — is
// UNREACHABLE without W9b's event. The locked criteria say: "So W9b builds
// before W9a, or W9a-15 is DEFERRED with W9b named as its carrier and W9a's
// gate records it OPEN rather than absent."
//
// I have written the test rather than deferring it, because a deferral is a
// BUILD-ORDER decision and this is a test-writer's deliverable. ⚠ W9a's GATE
// MUST STILL RECORD ITS STATE EXPLICITLY: if W9b has not landed, this test
// reds at the `POST /arrivals/unref` route check with W9b named, and that is
// an OPEN, not a W9a failure. The message below says so.
func TestW9a15TheClassBAmendmentThatOutlivesItsGate(t *testing.T) {
	srv, st := w9aDestinationServer(t, "w9a-15.db")
	far, near := New(w9aConfigWithSiteC(st.Path), st), srv
	_ = far
	w9aSeedPerson(t, st, "site-r", 1, "p-drv15", "Dana Driver")
	// site-b is the near destination (240 min); site-c is the far one.
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-15", "p-drv15", nil, 240)

	// A mis-clicked expected arrival CLOSES it.
	arrival, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: the mis-clicked expected arrival: %v", err)
	}
	if s, _ := domain.TripStateByID(st, trip.TripID); s == nil || s.State != domain.TripStateArrived {
		t.Fatalf("PRECONDITION (W9a-15): the mis-click did not close trip %s", trip.TripID)
	}

	// §4 PERMITS this Class-B amendment: X is `arrived`.
	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID,
		Destination: &w9aDestination{
			Kind: "controlled", Value: "site-c", DeadlineMinutes: w9aI64(90),
		},
	}.payload())

	// The mis-click is discovered. THIS IS W9b's SURFACE.
	if !w9aRouteExists(t, near, "POST", w9aArrivalUnrefPath) {
		t.Fatalf("W9a-15 is BLOCKED ON W9b: `POST %s` is not registered. That route is W9b's (F4 §3.6, item 16), and the world this criterion pins — "+
			"a Class-B amendment legally authored on an `arrived` trip that `arrival_ref_retracted` then returns to `open` — is UNREACHABLE without it. "+
			"⚠ W9a's GATE must record this criterion as OPEN with W9b named as its carrier, NOT as absent and NOT as passed (the locked dependency note "+
			"at the head of W9a's section says exactly that). Arrival to un-reference: %s", w9aArrivalUnrefPath, arrival.ArrivalID)
	}
	if _, authored := w9aAmend(t, near, st, w9aArrivalUnrefPath, map[string][]string{
		w9aFieldArrivalID: {arrival.ArrivalID},
	}); !authored {
		t.Fatalf("W9a-15: `POST %s` authored nothing — the trip cannot be reopened and the criterion's world cannot be built", w9aArrivalUnrefPath)
	}
	if s, _ := domain.TripStateByID(st, trip.TripID); s == nil || s.State != domain.TripStateOpen {
		t.Fatalf("PRECONDITION (W9a-15): the un-reference did not return trip %s to `open`", trip.TripID)
	}

	// --- the judgement: the SELECTION composes -------------------------
	nearExp := w9aExpectationIDs(t, st, "site-b")
	farExp := w9aExpectationIDs(t, st, "site-c")
	if w9aHas(nearExp, trip.TripID) {
		t.Fatalf("W9a-15 (m18 — the STALE-SELECTION SCANNER): site-b's scanner still watches trip %s after the record was corrected to site-c. "+
			"F4 §9.1 / §4 option (c): the corrected destination decides WHICH POST'S SCANNER watches the trip. site-b: %v", trip.TripID, nearExp)
	}
	if !w9aHas(farExp, trip.TripID) {
		t.Fatalf("W9a-15 (m18): site-c's scanner does NOT watch trip %s after the record was corrected to site-c — asserted BY CONTENTS, not by "+
			"absence alone. site-c: %v", trip.TripID, farExp)
	}
	// The countdown is the LITERAL 90, not 240.
	dl := w9aExpectationDeadline(t, st, "site-c", trip.TripID)
	if dl != 90 {
		t.Fatalf("W9a-15 (m18): the reopened trip's countdown is %d minutes, want the LITERAL 90. The board promises 90 and `Banner()` will not raise "+
			"the trip for 240 — two and a half hours in which every trip surface says the vehicle is overdue and NO ALERT ROW EXISTS ON ANY SCREEN", dl)
	}
}

// =========================================================================
// W9a-1 / W9a-3 / W9a-7 — the remaining rendered halves
// =========================================================================

// TestW9a1TheIncidentReadShowsTheAmendedDriverWithThePostAttributedChain —
// W9a-1's render half: "the amended driver renders effective on the incident
// read with the chain post-attributed".
func TestW9a1TheIncidentReadShowsTheAmendedDriverWithThePostAttributedChain(t *testing.T) {
	srv, st := w9aOriginServer(t, "w9a-1-render.db")
	w9aSeedPerson(t, st, "site-r", 1, "drv-was", "Wanda Was")
	w9aSeedPerson(t, st, "site-r", 1, "drv-now", "Nora Now")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-1R", "drv-was", nil, 240)
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: %v", err)
	}
	page := w9aGetOK(t, srv, "/trips/detail")
	if !strings.Contains(page, "Wanda Was") {
		t.Fatalf("W9a-1 PROBE DEFECT: the incident read does not name the RAW driver before any amendment, so it cannot show that it composed")
	}
	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-now"),
	}.payload())
	page = w9aGetOK(t, srv, "/trips/detail")
	if !strings.Contains(page, "Nora Now") {
		t.Fatalf("W9a-1 (m1 — zero resolution / m2 — the surviving raw reader): the D-5 INCIDENT READ does not show the amended driver. " +
			"F4 §7: \"the effective record answers D-5's question truthfully\" — who was driving, who was aboard")
	}
	if !strings.Contains(page, "Wanda Was") {
		t.Fatalf("W9a-1: the incident read no longer shows the SUPERSEDED driver anywhere. [[D-1]]: the chain preserves what the record said before, " +
			"with post attribution per amendment; an amendment that erases its predecessor is the editing fold on a page")
	}
	if !strings.Contains(strings.ToLower(w3PageText(page)), "pad site control") &&
		!strings.Contains(w3PageText(page), "site-b") {
		t.Fatalf("W9a-1: the chain is not POST-ATTRIBUTED — neither the amending post's display name nor its site id appears with the amendment. "+
			"Attribution is structural (`env.SiteID`), and it is the whole of what the chain can say about who corrected the record. Page text:\n%s",
			w3PageText(page))
	}
}

// TestW9a3TheRenderedMusterPageIsByteIdenticalAcrossAmendments — W9a-3's
// rendered half: "the rendered muster page (live + reprint, httptest)
// byte-identical across amendments".
//
// ⟦NO PRE-CHUNK RED⟧ — nothing composes yet. THE PLANT: (m3) the helpful
// fold. ⚠ CONTROLLED on every run: the same comparison is first shown to
// DETECT a real muster change (a genuine gate check-in), so "identical"
// is a judgement and not a constant.
// ⟦LOCK — §142 instrument repair, s104, 2026-09-03⟧ The two routes were one
// loop over a SHARED fixture with a `t.Fatalf` in it. ⟦was: "for _, path :=
// range []string{\"/muster\", \"/muster/print\"} {" over a fixture built once
// above the loop⟧. That concealed the whole print arm, in BOTH halves, and
// neither is visible while iteration 1 fails:
//
//   - the CONTROL re-checked in the SAME person. [MEASURED] it still reports a
//     difference — but the ONLY difference is a visit id inside the "Reprint
//     this exact sheet" href; roster, tallies and names are byte-identical. So
//     it is not a no-op that would redden — it is a control that PASSES on
//     link churn while blind to who is on the sheet. A FALSE GREEN.
//   - the AMENDMENT was inside the loop, so iteration 2 re-applied an
//     IDENTICAL amendment whose effect had already landed. [MEASURED] the
//     composed set does not move, so the print arm compared a state with
//     itself and could never fail.
//
// [MEASURED] on a FRESH fixture the print arm is NOT identical across a first
// amendment (Xavier Ecks 1 -> 0) — it carries the same criterion as the live
// route and was simply never being asked.
//
// Repaired as SUBTESTS with a fixture each: a fatal can no longer conceal a
// later route, and every route measures a FIRST check-in and a FIRST
// amendment. The assertions, their messages and the criterion are untouched.
func TestW9a3TheRenderedMusterPageIsByteIdenticalAcrossAmendments(t *testing.T) {
	for i, path := range []string{"/muster", "/muster/print"} {
		t.Run(path, func(t *testing.T) {
			srv, st := w9aOriginServer(t, fmt.Sprintf("w9a-3-render-%d.db", i))
			w9aSeedPerson(t, st, "site-r", 1, "p-drv3", "Dana Driver")
			w9aSeedPerson(t, st, "site-r", 1, "p-x3", "Xavier Ecks")
			w9aSeedPerson(t, st, "site-r", 1, "p-z3", "Zoe Zed")
			w9aCheckIn(t, st, "site-a", "p-z3")
			w9aCheckIn(t, st, "site-a", "p-x3")
			trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-3R", "p-drv3", []string{"p-x3"}, 240)

			before := w9aGetOK(t, srv, path)

			// CONTROL: the comparison detects a REAL muster change.
			w9aCheckIn(t, st, "site-a", "p-drv3")
			if w9aGetOK(t, srv, path) == before {
				t.Fatalf("W9a-3 CONTROL FAILED (%s): the rendered page was byte-identical across a REAL check-in. The comparison cannot see a muster "+
					"change, so its clean verdict below proves nothing", path)
			}
			baseline := w9aGetOK(t, srv, path)

			w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
				TripID: trip.TripID, OccupantsRemove: []string{"p-x3"}, OccupantsAdd: []string{"p-z3"},
			}.payload())

			if got := w9aGetOK(t, srv, path); got != baseline {
				t.Fatalf("W9a-3 (m3 — the helpful fold): the RENDERED muster page %s changed across amendments. F4 §7: \"No trip-fact correction ever "+
					"adds or removes a person from who-is-shown-on-site\" — the same posture [[D-28]]'s safety clause takes for merges, for the same "+
					"reason: the life-safety document must not move as a side effect of record editing", path)
			}
		})
	}
}

// TestW9a7TheAlertStaysLiveAcrossAnArrivalClaimCorrection — W9a-7's
// "the alert stays live" limb, which needs the deadline scanner and so
// cannot live in `package domain` (domain cannot import deadline).
//
// ⟦NO PRE-CHUNK RED⟧ — nothing composes. THE PLANT: (m12)/(m12b), the
// composed arrival claim read by the correlation. ⚠ CONTROLLED: the banner
// must carry the alert BEFORE the correction, so "still live" is a judgement.
func TestW9a7TheAlertStaysLiveAcrossAnArrivalClaimCorrection(t *testing.T) {
	srv, st, trip := w9aOverdueAtDestination(t)
	w9aSeedPerson(t, st, "site-r", 1, "p-rider7w", "Rita Rider")
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, "ICE TRUKC 7", "site-a", []string{"p-rider7w"}, nil, 0, "default")"⟧
	arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", "ICE TRUKC 7"), "site-a", []string{"p-rider7w"}, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: the mistyped unsolicited arrival: %v", err)
	}
	// ⭐ ⟦S121 §48 CONTROL — repair queue row A, 2026-09-04⟧ S107 site A19, and
	// the MISSING TWIN of the probe-defect guard three lines below. That guard
	// pins the banner half of "still live"; this one pins the arrival half.
	// S107 planted the pre-§50 literal "ICE TRUKC 7" on the line above and this
	// test STAYED GREEN — although under that plant the arrival's vehicle claim
	// is NIL, so there is no mistyped claim for the amendment below to correct
	// and W9a-7's subject has silently evaporated.
	s121RequireArrivalKeyedOnVehicleRecord(t, st, arr, "ICE TRUKC 7",
		"At THIS site the loss is the fixture's own subject: this test exists to show that CORRECTING "+
			"AN ARRIVAL'S VEHICLE CLAIM never re-runs correlation and never silences the live alert "+
			"(F4 §5 rule 4). With a nil claim there is nothing to correct, the amendment below is a "+
			"no-op dressed as a correction, and \"the alert stays live\" is not a judgement.")
	// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "strings.Contains(w9aBannerRegion(t, srv), trip.VehicleEntered)"⟧
	if !strings.Contains(w9aBannerRegion(t, srv), vehicleSnapshotText(trip.VehicleEntered)) {
		t.Fatalf("W9a-7 PROBE DEFECT: the banner does not carry the overdue trip before the correction, so \"still live\" is not a judgement")
	}
	w9aApplyNext(t, st, "site-c", 1, w2Base+2*w9aMinute, map[string]any{
		"type": w9aEvtArrivalAmended, "arrival_id": arr.ArrivalID, "vehicle_entered": trip.VehicleEntered,
	})
	// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "strings.Contains(w9aBannerRegion(t, srv), trip.VehicleEntered)"⟧
	if !strings.Contains(w9aBannerRegion(t, srv), vehicleSnapshotText(trip.VehicleEntered)) {
		t.Fatalf("W9a-7 (m12 — the arrival-claim re-bind): correcting the arrival's VEHICLE CLAIM to the trip's own vehicle SILENCED the live alert. "+
			"F4 §5 rule 4: a claim correction never re-runs correlation, never binds, never un-binds. Banner:\n%s", w3PageText(w9aBannerRegion(t, srv)))
	}
}

// =========================================================================
// W9a-16(a) (T16) — the scoped-read probe, ARRIVAL reader
// =========================================================================

var (
	w9aArrRowsDecoded atomic.Int64
	w9aArrProbeOnce   sync.Once
)

const w9aArrRowProbeFn = "w9a_arrival_rows_decoded_probe"

// TestW9a16ArrivalCompositionDecodesOnlyItsOwnArrivalsRows — W9a-16(a),
// arrival half. The instrument is `w5d_scoped_read_test.go`'s, cloned;
// the reader is exercised through §9.2 CONSUMER 9, the review lane, whose
// sole caller is the /trips page.
//
// KILLER MUTANT: (m27) the UNSCOPED COMPOSITION READ.
func TestW9a16ArrivalCompositionDecodesOnlyItsOwnArrivalsRows(t *testing.T) {
	w9aArrProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(w9aArrRowProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w9aArrRowsDecoded.Add(1)
				return args[0], nil
			})
	})
	srv, st := w9aDestinationServer(t, "w9a-16-arrival.db")
	w9aSeedPerson(t, st, "site-r", 1, "p-drv16b", "Dana Driver")

	const arrivals, perArrival = 4, 3
	for i := 0; i < arrivals; i++ {
		// Two open trips sharing one vehicle make the unsolicited arrival
		// AMBIGUOUS, which is a review-lane row (§9.2 consumer 9).
		v := fmt.Sprintf("TRK-16B-%d", i)
		w9aDispatchOpen(t, st, "site-a", "site-b", 1, v, "p-drv16b", nil, 600)
		w9aDispatchOpen(t, st, "site-a", "site-b", 1, v, "p-drv16b", nil, 600)
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, v, "site-a", []string{"p-drv16b"}, nil, 0, "default")"⟧
		arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", v), "site-a", []string{"p-drv16b"}, nil, 0, "default")
		if err != nil {
			t.Fatalf("w9a fixture: unsolicited arrival: %v", err)
		}
		for j := 0; j < perArrival; j++ {
			w9aApplyNext(t, st, "site-c", 1, w2Base+int64(j)*w9aMinute, map[string]any{
				"type": w9aEvtArrivalAmended, "arrival_id": arr.ArrivalID,
				"vehicle_entered": fmt.Sprintf("%s-rev%d", v, j),
			})
		}
	}

	cols := w9aColumnsOf(t, st, "arrival_amendment")
	if len(cols) == 0 {
		t.Fatalf("W9a-16(a), arrival half: table `arrival_amendment` does not exist, so the rows-decoded shadow view cannot be installed. " +
			"On the pre-chunk tree that IS this criterion's produced red")
	}
	sel := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == "origin_site" {
			sel = append(sel, w9aArrRowProbeFn+"(origin_site) AS origin_site")
			continue
		}
		sel = append(sel, c)
	}
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TEMP VIEW arrival_amendment AS SELECT " + strings.Join(sel, ", ") + " FROM main.arrival_amendment")
		return err
	}); err != nil {
		t.Fatalf("W9a-16(a) CONTROL FAILED (instrument): could not install the shadow view: %v", err)
	}

	before := w9aArrRowsDecoded.Load()
	w9aGetOK(t, srv, "/trips")
	decoded := w9aArrRowsDecoded.Load() - before
	total := int64(arrivals * perArrival)
	if decoded != total {
		msg := ""
		if decoded == int64(arrivals)*total {
			msg = " — that is the WHOLE TABLE once per arrival: the composition read is unscoped"
		}
		t.Fatalf("W9a-16(a), arrival half (m27 — the unscoped composition read): rendering the review lane decoded %d `arrival_amendment` rows over "+
			"%d arrivals, want %d (each arrival's own %d)%s. F4 §3.3: the composed readers are the `PersonByID` shape — a SCOPED read, filtered in SQL",
			decoded, arrivals, total, perArrival, msg)
	}
}

// =========================================================================
// the world, the probes, and the small helpers
// =========================================================================

func w9aConsumers() []w9aConsumer {
	page := func(get func(t *testing.T, fx *w9aWorld) string, region func(string) string) func(*testing.T, *w9aWorld) (string, error) {
		return func(t *testing.T, fx *w9aWorld) (string, error) {
			body := get(t, fx)
			if region == nil {
				return body, nil
			}
			r := region(body)
			if r == "" {
				return "", fmt.Errorf("the probe's region is empty — the surface no longer carries the section this row is bound to")
			}
			return r, nil
		}
	}
	trips := func(t *testing.T, fx *w9aWorld) string { return w9aGetOK(t, fx.dest, "/trips") }
	detail := func(t *testing.T, fx *w9aWorld) string { return w9aGetOK(t, fx.origin, "/trips/detail") }
	muster := func(t *testing.T, fx *w9aWorld) string { return w9aGetOK(t, fx.origin, "/muster") }
	// ⟦R-27, James, 2026-09-16⟧ /gate/vehicle, not /gate — the inbound
	// board ("On the road to") and incompleteCheckInRows ("Incomplete
	// check-ins") these two consumers bind to moved there, unchanged in
	// behaviour, when /gate became the streamlined individual page. See
	// gate_vehicle.html's own file comment.
	gate := func(t *testing.T, fx *w9aWorld) string { return w9aGetOK(t, fx.dest, "/gate/vehicle") }
	gateOrigin := func(t *testing.T, fx *w9aWorld) string { return w9aGetOK(t, fx.origin, "/gate/vehicle") }

	return []w9aConsumer{
		{N: 1, Name: "/trips table + trip detail (the D-5 incident read)", Reads: "driver, vehicle, destination, occupants, cargo note",
			Probe: page(detail, nil), Raw: "drv-c1-raw", Amend: "drv-c1-new", Shared: "shares /trips/detail with consumer 11"},
		{N: 2, Name: "/trips board rows", Reads: "DriverName, Occupants, OccupantRows (via OpenExpectations)",
			Probe: page(trips, func(b string) string { return w9aRegionByHeading(b, "Inbound") }), Raw: "drv-c2-raw", Amend: "drv-c2-new"},
		{N: 3, Name: "in-transit board", Reads: "Class-A facts live — driver, aboard set, vehicle",
			Probe: page(muster, func(b string) string { return w9aRegionByAttr(b, `data-muster-section="in-transit"`) }), Raw: "Cee Three Orig", Amend: "Cee Three Added"},
		{N: 4, Name: "inbound console manifest", Reads: "the advisory manifest (D-3): driver + occupant list",
			Probe: page(gate, func(b string) string { return w9aRegionByHeading(b, "On the road to") }), Raw: "Cee Four Orig", Amend: "Cee Four Added"},
		{N: 5, Name: "the deadline scanner's rendered Class-A fields", Reads: "Alert.Driver, Alert.Occupants, Alert.OverrideFace",
			Probe: page(trips, func(b string) string { return w9aRegionByClass(b, "banner-notice") }), Raw: "Cee Five Raw", Amend: "Cee Five New"},
		{N: 6, Name: "NotTracked", Reads: "occupant membership, driver, vehicle — plus merge resolution [[D-59]]",
			Probe: func(t *testing.T, fx *w9aWorld) (string, error) {
				return strings.Join(w9aNotTrackedNames(t, fx.st, fx.tripFor[6].TripID), "|"), nil
			}, Raw: "Cee Six Orig", Amend: "Cee Six Added"},
		{N: 7, Name: "OutAtUncontrolled", Reads: "occupants, driver, vehicle",
			Probe: func(t *testing.T, fx *w9aWorld) (string, error) {
				rows, err := domain.OutAtUncontrolled(fx.st)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, r := range rows {
					for _, p := range r.Persons {
						fmt.Fprintf(&b, "%s|", p.Name)
					}
				}
				return b.String(), nil
			}, Raw: "Cee Seven Orig", Amend: "Cee Seven Added"},
		{N: 8, Name: "the person picker's away grouping (transitively via 7)", Reads: "— (transitive)",
			// ⚠ SEAM: §9.2 names "the person picker's away grouping"; the
			// rendered away grouping fed by OutAtUncontrolledFrom is the
			// muster page's off-site panel, and that is where the composed
			// value is observable. Bound here, disputes to the gate.
			Probe: page(muster, func(b string) string { return w9aRegionByAttr(b, `data-muster-section="away"`) }),
			Raw:   "Cee Seven Orig", Amend: "Cee Seven Added", Shared: "transitive on consumer 7's fixture, per §9.2's own dash"},
		{N: 9, Name: "review lane rows (arrival claims + §3.6's ArrivalRefRetracted rows)", Reads: "arrival claims — effective value + §4's record-only sentence",
			Probe: page(trips, func(b string) string { return w9aRegionByHeading(b, "Needs a human") }), Raw: "VEH-C9-RAW", Amend: "VEH-C9-NEW",
			ControlDeferred: "the review lane does not render an arrival's CLAIM at all on the pre-chunk tree — the row prints the arrival id and the " +
				"candidate trip ids only. F4 §9.2 row 9 says so in terms: this consumer needs \"a fourth struct field, a fourth filtered scan and a " +
				"template block\". So the raw-marker control cannot exist before the build, and the discrimination is carried instead by the amended " +
				"marker VEH-C9-NEW differing from the raw VEH-C9-RAW: a build that renders the RAW claim shows VEH-C9-RAW and reds"},
		{N: 10, Name: "incompleteCheckInRows (gate recovery + muster, live and printed)", Reads: "vehicle, driver, occupant count",
			Probe: page(gateOrigin, func(b string) string { return w9aRegionByHeading(b, "Incomplete check-ins") }), Raw: "Cee Ten Raw", Amend: "Cee Ten New"},
		{N: 11, Name: "the audit/incident chain per trip", Reads: "every asserted value, with post attribution",
			// ⚠ THE MARKER IS THE SUPERSEDED INTERMEDIATE ASSERTION, on
			// purpose. Trip 1 is amended TWICE — raw -> mid -> new — so
			// `drv-c1-mid` is a value that is neither what the record said
			// first nor what it says now, and appears NOWHERE on any surface
			// except a chain that preserves what was asserted ([[D-1]]).
			// Using the effective value here would let consumer 1's own
			// render satisfy this row, and using a site id would let the
			// destination <select> satisfy it — both are false greens this
			// row already had once.
			Probe: page(detail, nil), Raw: "drv-c1-raw", Amend: "drv-c1-mid", Shared: "shares /trips/detail with consumer 1, distinct marker"},
	}
}

func w9aBuildWorld(t *testing.T) *w9aWorld {
	t.Helper()
	st := w3OpenRegistered(t, "w9a-2-world.db")
	fx := &w9aWorld{st: st, tripFor: map[int]domain.Trip{}}
	fx.origin = New(w9aConfigWithSiteC(st.Path), st)
	fx.dest = New(w3ConfigSiteB(st.Path), st)
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	fx.alerts = d
	fx.dest.Alerts = d

	seed := func(id, name string) { w9aSeedPerson(t, st, "site-r", 1, id, name) }
	seed("drv-c1-raw", "drv-c1-raw")
	seed("drv-c1-mid", "drv-c1-mid")
	seed("drv-c1-new", "drv-c1-new")
	seed("drv-c2-raw", "drv-c2-raw")
	seed("drv-c2-new", "drv-c2-new")
	seed("p-c3-orig", "Cee Three Orig")
	seed("p-c3-added", "Cee Three Added")
	seed("p-c4-orig", "Cee Four Orig")
	seed("p-c4-added", "Cee Four Added")
	seed("drv-c5-raw", "Cee Five Raw")
	seed("drv-c5-new", "Cee Five New")
	seed("p-c6-orig", "Cee Six Orig")
	seed("p-c6-added", "Cee Six Added")
	seed("p-c7-orig", "Cee Seven Orig")
	seed("p-c7-added", "Cee Seven Added")
	seed("drv-c10-raw", "Cee Ten Raw")
	seed("drv-c10-new", "Cee Ten New")
	seed("p-c9", "Cee Nine Rider")

	// 1 — an ARRIVED trip for the incident read and the chain.
	fx.tripFor[1] = w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-C1", "drv-c1-raw", nil, 240)
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, fx.tripFor[1].TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// 2 — an OPEN trip on the board.
	fx.tripFor[2] = w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-C2", "drv-c2-raw", nil, 600)
	// 3 / 4 — OPEN trips whose ABOARD SET the in-transit board and the
	// inbound manifest render.
	fx.tripFor[3] = w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-C3", "drv-c2-raw", []string{"p-c3-orig"}, 600)
	fx.tripFor[4] = w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-C4", "drv-c2-raw", []string{"p-c4-orig"}, 600)
	// 5 — the OVERDUE trip the banner renders.
	fx.tripFor[5] = w9aDispatchOpenOverdue(t, st, fx.dest, "TRK-C5", "drv-c5-raw", nil)
	// 6 — an ARRIVED trip with a missing occupant.
	fx.tripFor[6] = w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-C6", "drv-c2-raw", []string{"p-c6-orig"}, 240)
	if _, err := domain.RecordExpectedArrivalExcluding(st, "site-b", 1, fx.tripFor[6].TripID,
		[]string{"p-c6-orig"}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// 7 / 8 — a run to an UNCONTROLLED pad.
	unc := "conneco"
	tu, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-C7", DriverPersonID: "drv-c2-raw","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-C7"), DriverPersonID: "drv-c2-raw",
		OccupantPersonIDs: []string{"p-c7-orig"}, DestinationUncontrolled: &unc,
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: uncontrolled dispatch: %v", err)
	}
	fx.tripFor[7] = tu
	// 9 — an AMBIGUOUS unsolicited arrival: two open trips share a vehicle.
	w9aDispatchOpen(t, st, "site-a", "site-b", 1, "VEH-C9-RAW", "drv-c2-raw", nil, 600)
	w9aDispatchOpen(t, st, "site-a", "site-b", 1, "VEH-C9-RAW", "drv-c2-raw", nil, 600)
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, "VEH-C9-RAW", "site-a", []string{"p-c9"}, nil, 0, "default")"⟧
	arr, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", "VEH-C9-RAW"), "site-a", []string{"p-c9"}, nil, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: ambiguous arrival: %v", err)
	}
	fx.arrival = arr
	// 10 — an INCOMPLETE check-in at the origin: dispatched, never arrived.
	inc, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-C10", DriverPersonID: "drv-c10-raw","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-C10"), DriverPersonID: "drv-c10-raw",
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: incomplete check-in: %v", err)
	}
	fx.tripFor[10] = inc
	return fx
}

func w9aAmendTheWorld(t *testing.T, fx *w9aWorld) {
	t.Helper()
	at := int64(0)
	amend := func(p map[string]any) {
		at++
		w9aApplyNext(t, fx.st, "site-c", 1, w2Base+at*w9aMinute, p)
	}
	// Trip 1 is amended TWICE so consumer 11's marker (the SUPERSEDED
	// intermediate assertion) exists at all.
	amend(w9aTripAmend{TripID: fx.tripFor[1].TripID, Driver: w9aStr("drv-c1-mid")}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[1].TripID, Driver: w9aStr("drv-c1-new")}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[2].TripID, Driver: w9aStr("drv-c2-new")}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[3].TripID, OccupantsAdd: []string{"p-c3-added"}}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[4].TripID, OccupantsAdd: []string{"p-c4-added"}}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[5].TripID, Driver: w9aStr("drv-c5-new")}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[6].TripID, OccupantsAdd: []string{"p-c6-added"}}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[7].TripID, OccupantsAdd: []string{"p-c7-added"}}.payload())
	amend(w9aTripAmend{TripID: fx.tripFor[10].TripID, Driver: w9aStr("drv-c10-new")}.payload())
	amend(map[string]any{
		"type": w9aEvtArrivalAmended, "arrival_id": fx.arrival.ArrivalID, "vehicle_entered": "VEH-C9-NEW",
	})
}

// --- small helpers -------------------------------------------------------

func w9aGetOK(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := w2Get(t, srv, path, loginCookie(t, srv, testLoginPassword))
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// w9aRegionByHeading returns the page from the <h2> containing `heading` up
// to the next <h2>.
func w9aRegionByHeading(page, heading string) string {
	body := w2StripNonMarkup(page)
	for i := 0; ; {
		j := strings.Index(body[i:], "<h2")
		if j < 0 {
			return ""
		}
		start := i + j
		end := strings.Index(body[start+3:], "<h2")
		var seg string
		if end < 0 {
			seg = body[start:]
		} else {
			seg = body[start : start+3+end]
		}
		if strings.Contains(seg, heading) {
			return seg
		}
		if end < 0 {
			return ""
		}
		i = start + 3 + end
	}
}

// w9aRegionByAttr returns the element bearing `attr` up to its </section>.
func w9aRegionByAttr(page, attr string) string {
	body := w2StripNonMarkup(page)
	i := strings.Index(body, attr)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], "</section>")
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

// w9aRegionByClass joins every element whose class attribute names `class`.
func w9aRegionByClass(page, class string) string {
	body := w2StripNonMarkup(page)
	var out strings.Builder
	for i := 0; ; {
		j := strings.Index(body[i:], class)
		if j < 0 {
			break
		}
		start := i + j
		end := strings.Index(body[start:], "</div>")
		if end < 0 {
			out.WriteString(body[start:])
			break
		}
		out.WriteString(body[start : start+end])
		i = start + end
	}
	return out.String()
}

func w9aNotTrackedNames(t *testing.T, st *store.Store, tripID string) []string {
	t.Helper()
	rows, err := domain.NotTracked(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		if tripID != "" && r.TripID != tripID {
			continue
		}
		for _, p := range r.Persons {
			out = append(out, p.Name)
		}
	}
	return out
}

func w9aHas(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func w9aProjection(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	b, err := st.DumpProjection(name)
	if err != nil {
		t.Fatalf("DumpProjection(%q): %v", name, err)
	}
	return string(b)
}

func w9aBannerRegion(t *testing.T, srv *Server) string {
	t.Helper()
	return w9aRegionByClass(w9aGetOK(t, srv, "/trips"), "banner-notice")
}

// w9aDispatchOpenOverdue dispatches a one-minute trip and drives it past its
// deadline on the scanner's own monotonic clock — `alerts_test.go`'s shape.
func w9aDispatchOpenOverdue(t *testing.T, st *store.Store, srv *Server, vehicle, driver string, occupants []string) domain.Trip {
	t.Helper()
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, vehicle, driver, occupants, 1)
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("w9a fixture: fetching the trip envelope: %v", err)
	}
	srv.Alerts.OnApplied(envs[0], time.Now().Add(-time.Minute+80*time.Millisecond), true)
	time.Sleep(150 * time.Millisecond)
	return trip
}

func w9aExpectationIDs(t *testing.T, st *store.Store, siteID string) []string {
	t.Helper()
	es, err := domain.OpenExpectations(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Trip.TripID)
	}
	return out
}

func w9aExpectationDeadline(t *testing.T, st *store.Store, siteID, tripID string) int64 {
	t.Helper()
	es, err := domain.OpenExpectations(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Trip.TripID == tripID && e.Trip.DeadlineMinutes != nil {
			return *e.Trip.DeadlineMinutes
		}
	}
	return -1
}

func w9aColumnsOf(t *testing.T, st *store.Store, table string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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

func w9aCheckIn(t *testing.T, st *store.Store, siteID, personID string) {
	t.Helper()
	trip, err := domain.DispatchLocalCheckIn(st, siteID, 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: personID,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "BUS 7"), DriverPersonID: personID,
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: check-in dispatch for %s: %v", personID, err)
	}
	if _, err := domain.RecordExpectedArrival(st, siteID, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: check-in arrival for %s: %v", personID, err)
	}
}
