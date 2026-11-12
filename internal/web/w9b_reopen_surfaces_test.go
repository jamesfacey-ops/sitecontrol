// w9b_reopen_surfaces_test.go — W9b-1's halves that need a renderer or the
// deadline board, and the twin-arm confirmation the FA2-6 ruling substituted
// for W9c's un-ack tail.
//
//	the alert and board row are BACK              TestW9b1ReopenedTripAlertsAgain
//	THE PEER HALF (positive, cross-post)          TestW9b1PeerHalfTheDestinationGetsTheTripBack
//	THE MUSTER HALF, as locked — see the ⚠⚠ below TestW9b1MusterBytesAtTheDestination
//	                                              TestW9b1MusterBytesAtTheORIGIN
//	m24, carried independently of that conflict   TestW9b1TheMisTakenArrivalsCrewSTAYSOnTheMusterRoster
//	no alert while something else binds, + tail   TestW9b1NoAlertWhileSomethingElseBinds
//	the born-acked twin arms (m16)                TestW9b1BornAckedTwinArms
//
// ⚠⚠ A LOCKED-CRITERIA CONFLICT, MEASURED, REPORTED, NOT RESOLVED HERE — AND
// IT IS AT BOTH POSTS, FOR TWO INDEPENDENT REASONS.
//
// W9b-1's MUSTER HALF requires the rendered muster page to be "byte-identical
// across the retraction" at BOTH posts, live and reprint. W9b-1 ⟨1⟩ requires
// the same retraction to return the trip to `open`, with "its deadline
// expectation, board row and alert BACK". Those two cannot both hold:
//
//	AT THE DESTINATION — the C7 ALERT BANNER rides every page, the muster
//	included (`musterData.Banner`, set from `Server.banner()` on both the live
//	and the print route). ⟨1⟩ REQUIRES the alert to come back, so the banner
//	block appears in the page's bytes where it was absent before.
//	[MEASURED against a faithful scratch implementation: the destination's
//	`/muster` gained a `data-alert-row` notice and an `alert-count` paragraph
//	across the retraction, and nothing else changed.]
//
//	AT THE ORIGIN — additionally, [[D-6]]'s in-transit section is derived from
//	the OPEN trip-state set. [MEASURED on the delivered tree]
//	`InTransitFrom(st, "site-a")` returns ONE row for a dispatched trip while
//	it is open and ZERO once it is arrived; `sightedSince` clears an occupant
//	only on a visit at the ORIGIN site, and the mis-click's visits are at the
//	DESTINATION. So a reopen returns this crew to the origin's in-transit
//	section, which `musterData.InTransit` renders on both routes.
//
// Both arms are written below EXACTLY as locked, and both failure messages say
// all of this, so the gate is handed the conflict rather than a silent
// narrowing. Mutant m24 — the muster-render filter — is killed independently by
// TestW9b1TheMisTakenArrivalsCrewSTAYSOnTheMusterRoster, so its kill is not
// held hostage to an editorial ruling. See
// `reports/window-build/W9b-tests.md` §"STOPPED AND REPORTED".
//
// ⚠ NO PEER ASSERTION IN W9b MAY BE A BARE ABSENCE. W9b-1 says so in terms:
// an un-replicated retraction is green on every same-store half and on T13b's
// "`Banner()` empty at both posts", which is "a vacuous negative satisfied by a
// peer that received nothing at all". Every cross-post assertion here is
// therefore POSITIVE — the board row RETURNS, the alert FIRES, the lane row is
// PRESENT.
//
// KILLER MUTANTS: m14 (no-op un-bind), m21 (unconditional reopen), m23 (the
// unreplicated retraction), m24 (the muster-render filter), m16 (the
// unconditionally-reassuring confirmation).
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w9bOverdueMisClick builds a two-post world in which trip X is OPEN, OVERDUE
// and ALERTING at the destination, and is then mis-referenced there — the
// exact situation [[D-57]] is about: "a mis-click silences a live deadline
// expectation at both posts".
//
// Returns the posts, the trip and the mis-clicked arrival. Every step is
// asserted, because a fixture whose alert never fired makes "the alert is
// BACK" unfalsifiable.
func w9bOverdueMisClick(t *testing.T, name string) (*w9bPosts, domain.Trip, domain.Arrival) {
	t.Helper()
	p := w9bNewPosts(t, name)

	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "drv-o", "Olive Overdue")
	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "occ-o", "Percy Passenger")
	x := w9bDispatchOpen(t, p.OriginStore, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-o", []string{"occ-o"}, 1)

	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)
	w9bCaptureLiveOverdue(t, p.DestScan, p.DestStore, "site-a", 1, x.Seq)

	if w9bBannerFor(t, p.DestScan, x.TripID) == nil {
		t.Fatalf("PRECONDITION: trip %s is not on the DESTINATION's banner before the mis-click, so \"the alert is BACK\" below could never be observed. Banner: %s",
			x.TripID, w9bBannerSummary(t, p.DestScan))
	}

	a1, err := domain.RecordExpectedArrival(p.DestStore, "site-b", 1, x.TripID, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: the mis-click: %v", err)
	}
	w9bReplicate(t, p.DestStore, p.OriginStore, "site-b", 1)

	if w9bBannerFor(t, p.DestScan, x.TripID) != nil {
		t.Fatalf("PRECONDITION: the mis-click did not silence trip %s's alert at the destination — the gap W9b exists to close is not present in this fixture. Banner: %s",
			x.TripID, w9bBannerSummary(t, p.DestScan))
	}
	return p, x, a1
}

// =========================================================================
// W9b-1 ⟨1⟩ — the alert and the board row are BACK
// =========================================================================

// TestW9b1ReopenedTripAlertsAgain — ⟨1⟩'s "its deadline expectation, board row
// and alert BACK", at the surfaces that carry them.
//
// KILLER MUTANT: m14 — the no-op un-bind.
func TestW9b1ReopenedTripAlertsAgain(t *testing.T) {
	p, x, a1 := w9bOverdueMisClick(t, "w9b-alertback")

	// Authored on the WIRE, not through the surface: W9b-1 is about the
	// fold's consequences, so this red can never be read as "the route is
	// missing" (that is W9b-2's red).
	w9bApplyNext(t, p.DestStore, "site-b", 1, w2Base+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Pad Site Control")))

	if row := w9bBannerFor(t, p.DestScan, x.TripID); row == nil {
		t.Fatalf("W9b-1 ⟨1⟩ (m14 — the no-op un-bind): after the retraction, trip %s is NOT on the destination's banner. Banner: %s\n"+
			"F4 §3.6: the trip reverts to `open` — \"deadline expectation live again, board row back, scanner re-engaged\". [[D-57]]: \"Reopening errs toward alerting, which is the direction a life-safety muster should err\"",
			x.TripID, w9bBannerSummary(t, p.DestScan))
	}

	// The BOARD ROW, on the page an officer actually looks at.
	board := w2StripNonMarkup(w9bGet(t, p.Dest, w9bTripsPath))
	if !strings.Contains(board, x.TripID) {
		t.Fatalf("W9b-1 ⟨1⟩: trip %s is back on the banner but does not appear on `GET %s`. The board row is the surface the criterion names; a scanner that knows and a page that does not is a silent alert",
			x.TripID, w9bTripsPath)
	}
}

// =========================================================================
// W9b-1 — THE PEER HALF, positive
// =========================================================================

// TestW9b1PeerHalfTheDestinationGetsTheTripBack is the peer half in full: the
// retraction is authored at the ORIGIN and DELIVERED OVER THE WIRE, and all
// three assertions are made AT THE DESTINATION.
//
// KILLER MUTANT: m23 — the unreplicated retraction: a build whose
// `arrival_ref_retracted` folds and recomputes correctly LOCALLY but is
// excluded from the replication envelope (or included but not triggering the
// peer's scoped recompute). It is green on every same-store half of W9b-1,
// green on the confirmation tests, and green on any "banner empty at both
// posts" assertion — which is a vacuous negative satisfied by a peer that
// received nothing at all. Live, "the officer at the origin un-references and
// the destination's board never gets the trip back — the alarm stays off at
// exactly the post waiting for the vehicle."
func TestW9b1PeerHalfTheDestinationGetsTheTripBack(t *testing.T) {
	p, x, a1 := w9bOverdueMisClick(t, "w9b-peer")

	// Authored at the ORIGIN.
	w9bApplyNext(t, p.OriginStore, "site-a", 1, w2Base+w9bMinute, w9bRetraction(a1.ArrivalID, w9bStr("Ice Road")))

	// The destination must not know yet — otherwise "delivered over the wire"
	// is not what is being tested.
	if w9bBannerFor(t, p.DestScan, x.TripID) != nil {
		t.Fatalf("w9b FIXTURE DEFECT: the destination already shows trip %s before the retraction was replicated — the two stores are not independent", x.TripID)
	}

	// Delivered over the wire, exactly as the engine delivers it.
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)

	// (1) the board row RETURNS
	board := w2StripNonMarkup(w9bGet(t, p.Dest, w9bTripsPath))
	if !strings.Contains(board, x.TripID) {
		t.Fatalf("W9b-1 PEER HALF (m23 — the unreplicated retraction): after the origin's retraction was delivered, trip %s does not appear on the DESTINATION's `GET %s`",
			x.TripID, w9bTripsPath)
	}

	// (2) the alert FIRES
	if row := w9bBannerFor(t, p.DestScan, x.TripID); row == nil {
		t.Fatalf("W9b-1 PEER HALF (m23): trip %s is not on the DESTINATION's banner after the origin's retraction was delivered. Banner: %s\n"+
			"This is the assertion that must be POSITIVE: an un-replicated retraction leaves the alarm off at exactly the post that is waiting for the vehicle, and every negative assertion in this suite is green on it",
			x.TripID, w9bBannerSummary(t, p.DestScan))
	}

	// (3) the destination's ArrivalRefRetracted review-lane row is PRESENT
	rows := w9bReviewRows(w9bGet(t, p.Dest, w9bTripsPath))
	found := false
	for _, r := range rows {
		if strings.Contains(r, a1.ArrivalID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("W9b-1 PEER HALF: the DESTINATION renders no review-lane row for the retracted arrival %s. Rows: %v\n"+
			"The lane is derived from replicated state, so its absence at the peer is the same defect as the missing board row, on the surface the officer is sent to", a1.ArrivalID, rows)
	}
}

// =========================================================================
// W9b-1 — THE MUSTER HALF
// =========================================================================

// w9bMusterFixture builds a two-post world, replicates it both ways, and
// returns the posts, the trip and the mis-clicked arrival — with visits
// materialised at the destination.
func w9bMusterFixture(t *testing.T, name string) (*w9bPosts, domain.Trip, domain.Arrival) {
	t.Helper()
	p := w9bNewPosts(t, name)
	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "drv-m", "Mabel Muster")
	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "occ-m", "Nate Nobody")
	x := w9bDispatchOpen(t, p.OriginStore, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-m", []string{"occ-m"}, 240)
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)

	a1, err := domain.RecordExpectedArrival(p.DestStore, "site-b", 1, x.TripID, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: the mis-click: %v", err)
	}
	w9bReplicate(t, p.DestStore, p.OriginStore, "site-b", 1)
	return p, x, a1
}

// w9bRetractEverywhere authors the retraction at the origin and delivers it to
// the destination, then asserts THE REOPEN ACTUALLY HAPPENED at both stores.
//
// ⚠ THAT ASSERTION IS WHAT STOPS THE MUSTER TESTS BEING VACUOUS. On a build
// that ignores the event entirely, the muster pages are byte-identical because
// NOTHING happened — the strongest possible green for the weakest possible
// build. The byte comparison only means something downstream of a reopen that
// occurred.
func w9bRetractEverywhere(t *testing.T, p *w9bPosts, x domain.Trip, arrivalID string) {
	t.Helper()
	w9bApplyNext(t, p.OriginStore, "site-a", 1, w2Base+w9bMinute, w9bRetraction(arrivalID, w9bStr("Ice Road")))
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)
	for _, arm := range []struct {
		name string
		st   *store.Store
	}{{"origin", p.OriginStore}, {"destination", p.DestStore}} {
		s, err := domain.TripStateByID(arm.st, x.TripID)
		if err != nil || s == nil {
			t.Fatalf("w9b: no trip_state for %s at the %s: %v", x.TripID, arm.name, err)
		}
		if s.State != domain.TripStateOpen {
			t.Fatalf("W9b-1 MUSTER HALF, NON-DEGENERACY GUARD (m14 — the no-op un-bind): at the %s, trip %s reads %q after the retraction, want %q.\n"+
				"Byte-identity of a muster page across an event that did NOTHING is the strongest green the weakest build can get, so the reopen is asserted before any page is compared",
				arm.name, x.TripID, s.State, domain.TripStateOpen)
		}
	}
}

// TestW9b1MusterBytesAtTheDestination is the muster half at the post where
// mutant m24 lives.
//
// KILLER MUTANT: m24 — the muster-render filter: the render-seam variant the
// visit-table assertion cannot see. The table is left alone and the mistaken
// arrival's people are filtered out AT THE MUSTER RENDER, so the crew vanishes
// from the sheet the moment the reference is withdrawn — while the visit
// projection stays byte-identical and every domain-level assertion passes.
//
// F4 §3.6 rules the visits STAY: "the crew is still rendered on-site at the
// destination while X's expectation reopens at both posts — a designed,
// accepted inconsistency that is VISIBLE on the muster."
func TestW9b1MusterBytesAtTheDestination(t *testing.T) {
	p, x, a1 := w9bMusterFixture(t, "w9b-muster-dest")

	liveBefore := w9bMusterBytes(t, p.Dest, w9bMusterLive)
	printBefore := w9bMusterBytes(t, p.Dest, w9bMusterPrint)

	// The crew must be ON the sheet before the retraction, or "they stay" is
	// a claim about an empty page.
	for _, name := range []string{"Mabel Muster", "Nate Nobody"} {
		if !strings.Contains(liveBefore, name) {
			t.Fatalf("w9b FIXTURE DEFECT: %q is not on the destination's live muster before the retraction, so m24 would have nothing to filter out.\npage:\n%s", name, liveBefore)
		}
	}

	w9bRetractEverywhere(t, p, x, a1.ArrivalID)

	for _, arm := range []struct {
		path, before string
	}{
		{w9bMusterLive, liveBefore},
		{w9bMusterPrint, printBefore},
	} {
		after := w9bMusterBytes(t, p.Dest, arm.path)
		if after != arm.before {
			t.Fatalf("W9b-1 MUSTER HALF, DESTINATION ARM — ⚠ LOCKED-CRITERIA CONFLICT, NOT (necessarily) A BUILD DEFECT.\n"+
				"The DESTINATION's `%s` changed across the retraction.\n--- before ---\n%s\n--- after ---\n%s\n\n"+
				"W9b-1 requires BOTH of these, and they are incompatible on this page:\n"+
				"  (i)  ⟨1⟩ — the retraction returns trip %s to `open`, with \"its deadline expectation, board row and ALERT BACK\";\n"+
				"  (ii) THE MUSTER HALF — the rendered muster page is byte-identical across the retraction, live and reprint, at BOTH posts.\n"+
				"The C7 alert banner rides EVERY page, the muster included (`musterData.Banner`, filled from `Server.banner()` on both routes), so an alert that comes back necessarily "+
				"changes the muster page's bytes. [MEASURED against a faithful implementation: the only difference was the banner notice and its alert-count paragraph.]\n"+
				"This is reported in `reports/window-build/W9b-tests.md` for W9b's gate to rule on. Mutant m24 — the muster-render filter, which is what this half exists to kill — is carried, "+
				"unaffected, by TestW9b1TheMisTakenArrivalsCrewSTAYSOnTheMusterRoster",
				arm.path, arm.before, after, x.TripID)
		}
	}
}

// w9bMusterSection slices one `data-muster-section="…"` block out of a
// rendered muster page: from its marker to the next section marker, or to the
// end. The template documents these as "structural hooks for tests" in as many
// words, so this is the page's own handle rather than one invented here.
func w9bMusterSection(t *testing.T, page, section string) string {
	t.Helper()
	marker := `data-muster-section="` + section + `"`
	i := strings.Index(page, marker)
	if i < 0 {
		t.Fatalf("w9b: the muster page carries no %s section — the on-site roster is where the crew this test is about is rendered.\npage:\n%s", marker, page)
	}
	rest := page[i+len(marker):]
	if j := strings.Index(rest, `data-muster-section="`); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// TestW9b1TheMisTakenArrivalsCrewSTAYSOnTheMusterRoster is the m24 kill,
// carried WITHOUT the byte-identity claim the origin and destination arms both
// falsify (see this file's header and the ORIGIN arm's message).
//
// ⚠ THIS IS NOT A SUBSTITUTE FOR THE LOCKED MUSTER HALF and is not offered as
// one. The locked half is asserted, unnarrowed, by the two tests around it;
// they are reported to W9b's gate as a criteria conflict. This test exists so
// that mutant m24 — the muster-render filter — is killed whatever the gate
// rules, because m24 is a life-safety defect and its kill must not wait on an
// editorial ruling.
//
// It asserts what W9b-1's own sentence asserts: "the mistaken arrival's visits
// STAY, so the crew is still rendered on-site while X's expectation reopens".
// Three ways, at the DESTINATION, on live AND reprint:
//
//	(a) both crew members are still NAMED on the sheet;
//	(b) the on-site TALLY is unchanged;
//	(c) the on-site ROSTER SECTION is byte-identical — the strongest form of
//	    the locked claim that survives the banner and in-transit sections
//	    changing, which W9b-1 ⟨1⟩ REQUIRES them to do.
func TestW9b1TheMisTakenArrivalsCrewSTAYSOnTheMusterRoster(t *testing.T) {
	p, x, a1 := w9bMusterFixture(t, "w9b-muster-roster")

	before := map[string]string{}
	for _, path := range []string{w9bMusterLive, w9bMusterPrint} {
		before[path] = w9bMusterSection(t, w9bMusterBytes(t, p.Dest, path), "on-site")
		for _, name := range []string{"Mabel Muster", "Nate Nobody"} {
			if !strings.Contains(before[path], name) {
				t.Fatalf("w9b FIXTURE DEFECT: %q is not in the on-site roster of %s before the retraction, so m24 would have nothing to filter out.\nsection:\n%s", name, path, before[path])
			}
		}
	}

	w9bRetractEverywhere(t, p, x, a1.ArrivalID)

	for _, path := range []string{w9bMusterLive, w9bMusterPrint} {
		page := w9bMusterBytes(t, p.Dest, path)
		after := w9bMusterSection(t, page, "on-site")
		for _, name := range []string{"Mabel Muster", "Nate Nobody"} {
			if !strings.Contains(after, name) {
				t.Fatalf("W9b-1 (m24 — the muster-render filter): after the retraction, %q is GONE from the on-site roster of %s.\nsection:\n%s\n"+
					"F4 §3.6: the un-reference never touches the visit table (I-1) and the crew STAYS on the sheet. A render-seam filter takes a crew off a life-safety document on the strength of a "+
					"clerical correction, and leaves the `visit` projection byte-identical while it does it — so every domain-level assertion in this suite passes",
					name, path, after)
			}
		}
		if after != before[path] {
			t.Fatalf("W9b-1 (m24): the on-site ROSTER SECTION of %s changed across the retraction.\n--- before ---\n%s\n--- after ---\n%s",
				path, before[path], after)
		}
	}
}

// TestW9b1MusterBytesAtTheORIGIN carries W9b-1's locked "at BOTH posts" claim
// on the origin side, where a SECOND, independent contradiction is present.
//
// ⚠⚠ THIS TEST IS EXPECTED TO FAIL A CORRECT BUILD, and so is its destination
// twin. Both are written anyway, unnarrowed, because resolving a conflict
// between two locked statements is not this test-writer's to do. See this
// file's header for the measurements. The failure message below states the
// conflict so that a reader meeting this red is not sent hunting for a build
// defect that is not there.
func TestW9b1MusterBytesAtTheORIGIN(t *testing.T) {
	p, x, a1 := w9bMusterFixture(t, "w9b-muster-origin")

	liveBefore := w9bMusterBytes(t, p.Origin, w9bMusterLive)
	printBefore := w9bMusterBytes(t, p.Origin, w9bMusterPrint)

	w9bRetractEverywhere(t, p, x, a1.ArrivalID)

	for _, arm := range []struct {
		path, before string
	}{
		{w9bMusterLive, liveBefore},
		{w9bMusterPrint, printBefore},
	} {
		after := w9bMusterBytes(t, p.Origin, arm.path)
		if after != arm.before {
			t.Fatalf("W9b-1 MUSTER HALF, ORIGIN ARM — ⚠ LOCKED-CRITERIA CONFLICT, NOT (necessarily) A BUILD DEFECT.\n"+
				"The ORIGIN's `%s` changed across the retraction.\n--- before ---\n%s\n--- after ---\n%s\n\n"+
				"W9b-1 requires BOTH of these, and at the origin they are incompatible:\n"+
				"  (i)  ⟨1⟩ — the retraction returns trip %s to `open`;\n"+
				"  (ii) THE MUSTER HALF — the rendered muster page is byte-identical across the retraction, live and reprint, at BOTH posts.\n"+
				"[[D-6]]'s in-transit section of the muster is derived from the OPEN trip-state set (`InTransitFrom` walks `openTripStates`), and `sightedSince` clears an occupant only on a visit "+
				"at the ORIGIN site — the mis-click's visits are at the DESTINATION. So a reopen necessarily returns this crew to the origin's in-transit section, which `musterData.InTransit` renders "+
				"on both routes. [MEASURED on the delivered tree: `InTransitFrom(st, \"site-a\")` returns 1 row while the trip is open and 0 once it is arrived.]\n"+
				"This is reported in `reports/window-build/W9b-tests.md` for W9b's gate to rule on. The m24 kill the muster half exists for is carried, unaffected, by "+
				"TestW9b1MusterBytesAtTheDestination",
				arm.path, arm.before, after, x.TripID)
		}
	}
}

// =========================================================================
// W9b-1 ⟨2⟩(a) — "no alert at either post", with its positive control
// =========================================================================

// TestW9b1NoAlertWhileSomethingElseBinds is the alert half of ⟨2⟩(a), and it
// is written as a PAIR because the negative alone is vacuous: on any build
// that ignores the retraction, no alert fires either.
//
// Arm 1 (the negative): retract A1 while A2 still binds X → no banner row at
// either post.
// Arm 2 (THE POSITIVE CONTROL, W9b-1's own tail): retract A2 as well → the
// banner row is BACK at the destination.
func TestW9b1NoAlertWhileSomethingElseBinds(t *testing.T) {
	p := w9bNewPosts(t, "w9b-doubleref")
	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "drv-d", "Dana Double")
	w9bSeedPerson(t, p.OriginStore, "site-a", 1, "occ-d", "Otto Occupant")
	x := w9bDispatchOpen(t, p.OriginStore, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-d", []string{"occ-d"}, 1)
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)
	w9bCaptureLiveOverdue(t, p.DestScan, p.DestStore, "site-a", 1, x.Seq)
	if w9bBannerFor(t, p.DestScan, x.TripID) == nil {
		t.Fatalf("PRECONDITION: trip %s never alerted at the destination. Banner: %s", x.TripID, w9bBannerSummary(t, p.DestScan))
	}

	const a1ID, a2ID = "arr-D1", "arr-D2"
	w9bApplyNext(t, p.DestStore, "site-b", 1, w2Base+w9bMinute, domain.TripArrival{
		Type: "trip_arrival", ArrivalID: a1ID, TripRef: &x.TripID,
		Visits: []domain.ArrivalVisit{{VisitID: a1ID + "-v0", PersonID: "drv-d"}}, RecordedBy: w9bStr("Pad Site Control"),
	})
	w9bApplyNext(t, p.DestStore, "site-b", 1, w2Base+2*w9bMinute, domain.TripArrival{
		Type: "trip_arrival", ArrivalID: a2ID, TripRef: &x.TripID,
		Visits: []domain.ArrivalVisit{{VisitID: a2ID + "-v0", PersonID: "occ-d"}}, RecordedBy: w9bStr("Pad Site Control"),
	})
	w9bReplicate(t, p.DestStore, p.OriginStore, "site-b", 1)

	// --- ARM 1: the negative -------------------------------------------
	w9bApplyNext(t, p.OriginStore, "site-a", 1, w2Base+3*w9bMinute, w9bRetraction(a1ID, w9bStr("Ice Road")))
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)

	if row := w9bBannerFor(t, p.DestScan, x.TripID); row != nil {
		t.Fatalf("W9b-1 ⟨2⟩(a) (m21 — the unconditional reopen): retracting %s while %s still binds trip %s put it back on the destination's banner as %q.\n"+
			"[[D-58]]: the trip stays closed where something else still BINDS it. Alerting for a vehicle whose crew a second post checked in is a false alarm on a life-safety board",
			a1ID, a2ID, x.TripID, row.Status)
	}
	if row := w9bBannerFor(t, p.OriginScan, x.TripID); row != nil {
		t.Fatalf("W9b-1 ⟨2⟩(a): the same false alarm at the ORIGIN (%q)", row.Status)
	}

	// --- ARM 2: THE POSITIVE CONTROL, without which arm 1 is vacuous ----
	w9bApplyNext(t, p.OriginStore, "site-a", 1, w2Base+4*w9bMinute, w9bRetraction(a2ID, w9bStr("Ice Road")))
	w9bReplicate(t, p.OriginStore, p.DestStore, "site-a", 1)

	if row := w9bBannerFor(t, p.DestScan, x.TripID); row == nil {
		t.Fatalf("W9b-1 ⟨2⟩(a) TAIL — THE POSITIVE CONTROL FAILED: with BOTH references retracted, trip %s is still not on the destination's banner. Banner: %s\n"+
			"Without this arm, arm 1's silence is satisfied by a build that never reopens anything — including a build that ignores the event entirely, which is m14",
			x.TripID, w9bBannerSummary(t, p.DestScan))
	}
}

// =========================================================================
// The born-acked twin arms — m16, and the FA2-6 ruling's native control
// =========================================================================

// TestW9b1BornAckedTwinArms is F4 §12 T13b plus the twin arm W9b's FA2-6
// ruling requires in place of serialising behind W9c.
//
// THE RULING, quoted because this test's shape is entirely determined by it:
// "W9b's certification now rests on a NATIVE positive control instead: the same
// confirmation surface, in the same fixture and the same run, on an
// otherwise-identical reopened trip carrying NO standing ack, asserted to
// render the 'OPEN — its alert is live now' copy with a NON-EMPTY `Banner()`.
// The two arms differ by EXACTLY ONE ACK, so the suppression is demonstrated
// to be the ack's doing without removing it."
//
// ⚠ THE F6 UN-ACK TAIL IS DELIBERATELY ABSENT. The ruling: "The F6 un-ack tail
// is an integration observation, not a W9b gate condition, and W9b's gate may
// NOT book it." Un-ack re-arming is certified natively by W9c-1/W9c-2.
//
// ⚠ WHERE THIS CRITERION LIVES. m16 is in W9b's twelve-member register and is
// the only one of the twelve NOT named in W9b's "Owned tests / must fail on"
// line; it is carried instead by the FA2-6 ruling paragraph, which is locked
// text in W9b's section. Reported as such.
//
// KILLER MUTANT: m16 — the unconditionally-reassuring confirmation: copy that
// asserts "its alert returns if it is overdue" in the standing-ack world, so
// the officer walks away believing a silenced alarm is live.
func TestW9b1BornAckedTwinArms(t *testing.T) {
	st := w3OpenRegistered(t, "w9b-bornacked.db")
	srv := New(w3ConfigSiteB(st.Path), st)
	scan := w9bWireScanner(t, srv, st, "site-b")
	if !w9bRouteExists(t, srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-1 / m16: `POST %s` is NOT REGISTERED, so neither arm's confirmation can be read. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}

	w9bSeedPerson(t, st, "site-a", 1, "drv-ack", "Ada Acked")
	w9bSeedPerson(t, st, "site-a", 1, "drv-live", "Liv Live")

	// TWO otherwise-identical overdue trips. They differ by EXACTLY ONE ACK.
	xAck := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "ACK TRUCK", "drv-ack", nil, 1)
	xLive := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "LIVE TRUCK", "drv-live", nil, 1)
	w9bCaptureLiveOverdue(t, scan, st, "site-a", 1, xAck.Seq)
	w9bCaptureLiveOverdue(t, scan, st, "site-a", 1, xLive.Seq)
	for _, x := range []domain.Trip{xAck, xLive} {
		if w9bBannerFor(t, scan, x.TripID) == nil {
			t.Fatalf("PRECONDITION: trip %s never went overdue. Banner: %s", x.TripID, w9bBannerSummary(t, scan))
		}
	}

	// The ONE difference between the arms.
	if err := deadline.Acknowledge(st, "site-b", 1, xAck.TripID, w9bStr("Pad Site Control"), 0, "default"); err != nil {
		t.Fatalf("w9b fixture: acknowledge: %v", err)
	}
	if w9bBannerFor(t, scan, xAck.TripID) != nil {
		t.Fatalf("PRECONDITION: the acknowledgment did not suppress trip %s. Banner: %s", xAck.TripID, w9bBannerSummary(t, scan))
	}

	// Both are mis-referenced, then both are un-referenced through the same
	// surface, in the same run.
	aAck, err := domain.RecordExpectedArrival(st, "site-b", 1, xAck.TripID, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: mis-click on the acked trip: %v", err)
	}
	aLive, err := domain.RecordExpectedArrival(st, "site-b", 1, xLive.TripID, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: mis-click on the un-acked trip: %v", err)
	}

	_, ackPage, ackCode, _ := w9bUnref(t, srv, st, url.Values{w9bFieldArrival: {aAck.ArrivalID}})
	_, livePage, liveCode, _ := w9bUnref(t, srv, st, url.Values{w9bFieldArrival: {aLive.ArrivalID}})
	if ackCode != 303 || liveCode != 303 {
		t.Fatalf("W9b-1 / m16 PRECONDITION: the two un-references returned %d and %d, want 303 each", ackCode, liveCode)
	}
	ackBody := w2StripNonMarkup(ackPage)
	liveBody := w2StripNonMarkup(livePage)

	// --- THE POSITIVE ARM, first: the twin without an ack ---------------
	// FA2-6's point exactly — it is what stops the acked arm's empty banner
	// being a vacuous negative.
	if row := w9bBannerFor(t, scan, xLive.TripID); row == nil {
		t.Fatalf("W9b-1 / m16 — THE NATIVE POSITIVE CONTROL FAILED: the un-acked twin %s is NOT on the banner after its un-reference. Banner: %s\n"+
			"The two arms differ by exactly one ack; if the alert does not return HERE, the acked arm's silence proves nothing about the ack", xLive.TripID, w9bBannerSummary(t, scan))
	}
	if !w9bSaysOpen(liveBody) {
		t.Fatalf("W9b-1 / m16: the un-acked twin's confirmation does not render the OPEN copy.\npage:\n%s", liveBody)
	}
	if w9bSaysSuppressed(liveBody) {
		t.Fatalf("W9b-1 / m16 (inverted): the un-acked twin's confirmation claims the alert is SUPPRESSED, and it is live. A cry-wolf suppression notice on a live alarm is the mirror of m16 and just as false.\npage:\n%s", liveBody)
	}

	// --- THE ACKED ARM --------------------------------------------------
	if row := w9bBannerFor(t, scan, xAck.TripID); row != nil {
		t.Fatalf("W9b-1 / m16: the reopened acked trip %s is on the banner as %q. F6 §1: the ack read is a trip-scoped ∃ with no state and no time filter whose rows survive arrival, "+
			"so a trip acked while overdue and reopened later is BORN ACKED and suppressed at both posts", xAck.TripID, row.Status)
	}
	if !w9bSaysSuppressed(ackBody) {
		t.Fatalf("W9b-1 (m16 — the unconditionally-reassuring confirmation): the acked trip's confirmation does not say the alert is SUPPRESSED.\npage:\n%s\n"+
			"F4 §3.6's third rendering: \"Trip ⟨X⟩ is OPEN again and it is overdue — but it carries a standing acknowledgment, so its alert stays SUPPRESSED at both posts until it is un-acknowledged.\" "+
			"The officer must never walk away believing a silenced alarm is live (SEAM W9B-S-COPY)", ackBody)
	}
	if w9bClaimsTheAlertIsLive(ackBody) {
		t.Fatalf("W9b-1 (m16): the acked trip's confirmation claims the alert is LIVE while a standing acknowledgment suppresses it.\npage:\n%s", ackBody)
	}

	// The /trips row independently witnesses that the reopen HAPPENED — the
	// assertion FA2-6 names as what stops the acked arm's empty banner being
	// a build that never reopened anything.
	trips := w2StripNonMarkup(w9bGet(t, srv, w9bTripsPath))
	if !strings.Contains(trips, xAck.TripID) {
		t.Fatalf("W9b-1 / m16: the reopened acked trip %s does not appear on `GET %s` at all. Its empty banner is then satisfied by a build that never reopened it — the vacuous negative FA2-6 names",
			xAck.TripID, w9bTripsPath)
	}
	if !w9bSaysAcknowledged(trips) {
		t.Fatalf("W9b-1 / m16: `GET %s` does not render trip %s as acknowledged. T13b: the /trips row reads \"OVERDUE (acknowledged)\" — it is the independent witness that the trip is overdue AND suppressed, "+
			"rather than simply not overdue.\npage:\n%s", w9bTripsPath, xAck.TripID, trips)
	}
}

func w9bSaysSuppressed(body string) bool {
	return strings.Contains(strings.ToUpper(body), "SUPPRESS")
}

func w9bSaysAcknowledged(body string) bool {
	u := strings.ToUpper(body)
	return strings.Contains(u, "ACKNOWLEDGED") || strings.Contains(u, "ACKED")
}

// w9bClaimsTheAlertIsLive is m16's own sentence, in the shapes F4 rev 3's
// condemned copy used.
func w9bClaimsTheAlertIsLive(body string) bool {
	u := strings.ToUpper(body)
	return strings.Contains(u, "ALERT IS LIVE") || strings.Contains(u, "ALERT RETURNS") || strings.Contains(u, "ITS ALERT IS BACK")
}
