// w9a_amend_surface_test.go — W9a's AUTHORING-surface criteria:
//
//	W9a-9  (T8) the authoring gate, all FOUR ways produced
//	W9a-10 (T9) the unsighted-removal warning + the cry-wolf control
//	W9a-4  (T4) render half — the LOSING post's adverse sentence, on a PAGE
//	W9a-5  (T5) render half — the disjoint edits carry NO supersession
//	            language, positive-controlled against the SAME surface
//	W9a-6  (T6) render half — the §3.4 invariant-override sentence
//
// See internal/domain/w9a_seams_test.go for the one design decision every
// W9a file rests on, and w9a_web_seams_test.go for SEAM W9A-S-FORM.
//
// ⚠ [[D-62]] IS NOT PRE-EMPTED HERE. D-62 transfers ONE question to W9's
// BUILD CRITIQUE: whether W9's three confirmation screens use one wording
// style or three. Nothing below asserts a wording STYLE. Every copy check is
// on SEMANTIC CONTENT the criteria and F4 §4/§7 specify — that a refusal
// tells the officer to wait until the trip arrives ([[D-78]]; ⟦was: "recommends
// cancel+redispatch"⟧) and never recommends a cancel; that it names radio verification;
// that it names origin-only-ness; that it names the check-in path; that a
// losing confirmation carries the adverse register. A builder may satisfy all
// of them in one voice or in three.
//
// ⚠ EVERY REFUSAL IS ASSERTED ON THE LOG, NEVER ON A STATUS CODE. This
// house's catch-all answers 200 to a great deal, and "nothing was authored"
// is a watermark fact.
package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- the copy detectors, and why each is shaped as it is ----------------

// w9aWaitRecommendation matches the COUNTDOWN copy's instruction since
// [[D-78]] (2026-09-05) — "Wait until it arrives and the arrival is
// recorded", the production constant classBWaitSentence, rendered.
// [[D-73]], James, verbatim: "no trip will be cancled they will continue on
// to the next post then leave that post with a second trip". The advice is
// TRUE: after arrival the vehicle, destination and allowance are all
// amendable from either post [MEASURED — reports/witness/d73-cancel-surface.md §3].
//
// It is the IMPERATIVE PAIR again, for the reason the old detector was: the
// OVERDUE copy also talks about arrival ("can be corrected once the trip is
// completed"), so a bare token search would not tell the two arms apart.
var w9aWaitRecommendation = regexp.MustCompile(`(?is)\bwait\s+until\s+it\s+arrives\b`)

// w9aCancelRecommendation matches the SUPERSEDED countdown instruction —
// "Cancel it and re-dispatch with the correct ⟨fact⟩". Until D-78 it was
// way 1's POSITIVE; since D-78 it is a NEGATIVE on every arm: no refusal may
// recommend the operation D-73 removed. Two clicks from that sentence used
// to take three people off the printed muster (amendment 192, reproduced
// twice — reports/witness/_ITEM-5-EVIDENCE.md).
//
// ⚠ IT MUST NOT BE A SEARCH FOR THE WORD "cancel". The OVERDUE copy contains
// that word too — "Do NOT cancel it to fix the record" — so a bare token
// search would report the overdue refusal as recommending exactly what it
// forbids. The detector is therefore the IMPERATIVE PAIR.
//
// ⚠ AND A NEGATIVE WHOSE DETECTOR NOTHING IN PRODUCTION CAN TRIP IS
// UNCONTROLLED. Before D-78, way 1 was the positive control — "what proves
// the detector fires at all, so way 2's negative means something". D-78
// removed that sentence from production, so the control MOVED: the
// superseded copy is pinned verbatim below and the detector-control arm
// fires the detector at it before any negative is allowed to count.
var w9aCancelRecommendation = regexp.MustCompile(`(?is)\bcancel\b(?:\s+\w+){0,6}?\s+and\s+re-?dispatch`)

// w9aSupersededCountdownCopy is F4 §4's rendering 1 AS BUILT before D-78 —
// trip_amend.go:465-470 at 42f45b8, vehicle variant, verbatim. It exists
// ONLY to prove w9aCancelRecommendation still fires. It must never be
// rendered again; a test that finds it on a page has found the D-73 defect.
const w9aSupersededCountdownCopy = "this trip is open and counting down, and the vehicle drives the running timer and the destination's board while it is. Cancel it and re-dispatch with the correct vehicle — the timer and the board must match the record"

// w9aNegatedCancel matches the overdue copy's refusal of that instruction,
// so the two arms are distinguished by what they SAY and not by whether a
// word occurs.
var w9aNegatedCancel = regexp.MustCompile(`(?is)\b(do\s+not|don't|never)\s+cancel`)

// w9aCorrectAfterArrival matches the non-origin rendering's promise since
// D-78 — "Once its arrival is recorded, the correction can be made here" —
// the half that makes the wait advice ACTIONABLE at a post that is not the
// origin. [MEASURED — d73-cancel-surface §3: an arrived trip's vehicle was
// amended AT THE DESTINATION and authored.]
var w9aCorrectAfterArrival = regexp.MustCompile(`(?is)once\s+(?:the|its)\s+arrival\s+is\s+recorded`)

var (
	w9aRadioVerify = regexp.MustCompile(`(?i)\bradio\b`)
	w9aCheckInCopy = regexp.MustCompile(`(?is)(gate\s+check-?in|check-?in,\s+not\s+a\s+trip)`)
	w9aOriginOnly  = regexp.MustCompile(`(?is)origin`)
)

// =========================================================================
// W9a-9 (T8) — the authoring gate, all four ways
// =========================================================================

// TestW9a9TheAuthoringGateAllFourWays — W9a-9 (T8), as re-ruled by [[D-78]].
//
// KILLER MUTANTS: (m8) SILENT ACCEPT on open trips; (m8b) the
// REFUSAL-THAT-AUTHORS; (m8c) — since D-78 — ANY CANCEL RECOMMENDATION, on
// any arm ⟦was: "the UNCONDITIONAL CANCEL RECOMMENDATION — the one that
// channels an officer into silencing a live alert at 03:00"⟧, and the DEAD
// END — a countdown refusal that names no working path; (m8d) the PHANTOM
// AWAY-ENTRY — a `DestinationAmend` composed onto a degenerate gate
// check-in, which F4 §3.5 calls "uncorrectable by this design's own rules".
func TestW9a9TheAuthoringGateAllFourWays(t *testing.T) {
	t.Run("detector-control-the-superseded-imperative-pair", func(t *testing.T) {
		// ⟦D-78, 2026-09-05⟧ Before D-78 this control lived inside way 1: the
		// countdown copy matched w9aCancelRecommendation, which proved the
		// detector fires, "so way 2's negative below means something". D-78
		// took that sentence out of production, so BOTH cancel negatives
		// (way 1's, way 2's, and way 3's) would now be satisfied by a detector
		// that matches nothing at all. The superseded copy is therefore pinned
		// verbatim and the detector is fired at it here.
		if !w9aCancelRecommendation.MatchString(w9aSupersededCountdownCopy) {
			t.Fatalf("W9a-9 DETECTOR CONTROL: w9aCancelRecommendation does not match the superseded countdown copy it was written for — " +
				"every cancel negative in this test is uncontrolled. Superseded copy:\n" + w9aSupersededCountdownCopy)
		}
		// And the two directions do not bleed into each other: the old
		// sentence never forbade cancelling, and it never told anyone to wait
		// — so the negated-cancel and wait detectors must NOT fire on it, or
		// way 1's new positive could be satisfied by the old defect.
		if w9aNegatedCancel.MatchString(w9aSupersededCountdownCopy) {
			t.Fatalf("W9a-9 DETECTOR CONTROL: w9aNegatedCancel matches the superseded countdown copy, which never forbade cancelling — the detector over-matches")
		}
		if w9aWaitRecommendation.MatchString(w9aSupersededCountdownCopy) {
			t.Fatalf("W9a-9 DETECTOR CONTROL: w9aWaitRecommendation matches the superseded countdown copy — way 1's D-78 positive could be satisfied by the pre-D-78 defect")
		}
	})

	t.Run("way1-open-counting-down-at-the-origin", func(t *testing.T) {
		srv, st := w9aOriginServer(t, "w9a-9-way1.db")
		w9aRequireAmendRoute(t, srv)
		w9aSeedPerson(t, st, "site-r", 1, "p-drv9", "Dana Driver")
		trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-9A", "p-drv9", nil, 240)

		page, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID: {trip.TripID}, w9aFieldVehicle: {"TRK-9A-CORRECTED"},
		})
		text := w3PageText(page)
		if authored {
			t.Fatalf("W9a-9 (m8 — silent accept / m8b — the refusal-that-authors): a Class-B vehicle amendment on an OPEN, counting-down trip AUTHORED. "+
				"F4 §4: Class-B facts are correctable only on a COMPLETED trip — on an open one they drive live machinery and a record-only "+
				"correction makes the record disagree with the machinery still running on the raw values. Page:\n%s", page)
		}
		// ⟦D-78, 2026-09-05⟧ THE POSITIVE MOVED, and the old positive became a
		// negative. ⟦was: "if !w9aCancelRecommendation.MatchString(text) { … does not name cancel+redispatch …"⟧
		if !w9aWaitRecommendation.MatchString(text) {
			t.Fatalf("W9a-9 / D-78: the refusal on an OPEN, counting-down trip at the ORIGIN does not tell the officer to WAIT UNTIL IT ARRIVES. "+
				"D-73: \"no trip will be cancled they will continue on to the next post\"; D-78: the refusal says wait until it arrives, then correct it — "+
				"and that path is TRUE: after arrival the vehicle, destination and allowance are all amendable from either post [MEASURED, d73-cancel-surface §3]. "+
				"A refusal that does not name the working path is a dead end for the officer. Page text:\n%s", text)
		}
		if w9aCancelRecommendation.MatchString(text) {
			t.Fatalf("W9a-9 / D-78 (m8c): the countdown refusal STILL recommends cancel+redispatch — the instruction D-73 forbids and D-78 superseded. "+
				"Two clicks from this sentence used to take three people off the printed muster (amendment 192). Page text:\n%s", text)
		}
		// DETECTOR CONTROL, purpose unchanged: the two arms are told apart by
		// what they SAY — the countdown arm must not also carry the overdue
		// arm's prohibition. (The control that proves w9aCancelRecommendation
		// fires at all is the first arm of this test since D-78.)
		if w9aNegatedCancel.MatchString(text) {
			t.Fatalf("W9a-9 DETECTOR CONTROL: the countdown refusal carries the overdue arm's \"do not cancel\" — the two renderings are no longer distinguishable by what they say. Page text:\n%s", text)
		}
	})

	t.Run("way2-open-overdue-with-a-live-alert", func(t *testing.T) {
		srv, st, trip := w9aOverdueAtDestination(t)
		w9aRequireAmendRoute(t, srv)

		page, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID: {trip.TripID}, w9aFieldVehicle: {"ICE TRUCK 7 CORRECTED"},
		})
		text := w3PageText(page)
		if authored {
			t.Fatalf("W9a-9 (m8): a Class-B amendment on an OVERDUE OPEN trip AUTHORED. Page:\n%s", page)
		}
		// Controlled by the detector-control arm above (D-78 moved the
		// positive out of production; the detector is fired at the pinned
		// superseded copy instead).
		if w9aCancelRecommendation.MatchString(text) {
			t.Fatalf("W9a-9 (m8c — the UNCONDITIONAL CANCEL RECOMMENDATION): the refusal on an OVERDUE trip with a LIVE ALERT recommends cancel+redispatch. "+
				"F4 §4 / [[W9 verdict H-2]]: cancelling removes the alert AND the board row entirely, with nothing left on any screen for a vehicle that "+
				"may be missing, and a re-dispatch mints a fresh full countdown. This copy channels the officer into silencing a live alert at 03:00. Page text:\n%s", text)
		}
		if !w9aRadioVerify.MatchString(text) {
			t.Fatalf("W9a-9: the refusal on an OVERDUE trip does not name radio verification. F4 §4's second rendering: \"Verify the vehicle by radio and "+
				"resolve the alert first\". Without it the refusal takes the working path away and offers none. Page text:\n%s", text)
		}
	})

	t.Run("way3-rendered-at-the-destination", func(t *testing.T) {
		srv, st, trip := w9aOverdueAtDestination(t)
		w9aRequireAmendRoute(t, srv)
		page, _ := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID: {trip.TripID}, w9aFieldVehicle: {"ICE TRUCK 7 CORRECTED"},
		})
		text := w3PageText(page)
		// ⟦D-78, 2026-09-05⟧ RE-POINTED. ⟦was: the third rendering named
		// origin-only-ness because cancel was origin-only and the countdown
		// instruction rendered here would have INSTRUCTED THE IMPOSSIBLE —
		// "Cancellation is recorded at the origin post only — this post cannot
		// cancel ⟨origin⟩'s trip; radio ⟨origin⟩ if it must be stood down"⟧
		// Since D-78 there is nothing to cancel at ANY post. What the officer
		// at a non-origin post is owed instead: WHOSE trip this is (the origin,
		// named), and that once the arrival is recorded the correction can be
		// made HERE — it does not have to go through the origin. Both halves
		// are true [MEASURED — d73-cancel-surface §3, amended at the destination].
		if !w9aOriginOnly.MatchString(text) {
			t.Fatalf("W9a-9 / D-78: the refusal RENDERED AT A POST THAT IS NOT THE ORIGIN does not say so. The officer must be told whose trip "+
				"this is before being told where it can be corrected. Page text:\n%s", text)
		}
		// The origin is NAMED, not merely alluded to.
		originName := w9aPeerDisplayName(srv, "site-a")
		if originName != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(originName)) {
			t.Fatalf("W9a-9 / D-78: the non-origin refusal does not NAME the origin post (%q) — \"the vehicle is on the road from ⟨origin⟩\" has an "+
				"⟨origin⟩ in it for a reason. Page text:\n%s", originName, text)
		}
		// D-78 NEGATIVES: no cancel by any route — not the button, not the radio.
		if w9aCancelRecommendation.MatchString(text) {
			t.Fatalf("W9a-9 / D-78 (m8c): the non-origin refusal recommends cancel+redispatch. Page text:\n%s", text)
		}
		for _, stale := range []string{"stood down", "cannot cancel"} {
			if strings.Contains(text, stale) {
				t.Fatalf("W9a-9 / D-78: the non-origin refusal still talks about cancelling (%q) — the superseded third rendering told the officer to "+
					"radio the origin to stand the trip down, which is the cancel by another door. Page text:\n%s", stale, text)
			}
		}
		// D-78 POSITIVE: the correction path AT THIS POST is named, and it is true.
		if !w9aCorrectAfterArrival.MatchString(text) {
			t.Fatalf("W9a-9 / D-78: the non-origin refusal does not tell the officer that once the arrival is recorded the correction can be made HERE. "+
				"[MEASURED — d73-cancel-surface §3: an arrived trip's vehicle was amended at the destination and authored.] Without it the wait advice "+
				"reads as \"wait, then ask ⟨origin⟩\", which is false. Page text:\n%s", text)
		}
	})

	t.Run("way4-degenerate-gate-check-in-trip", func(t *testing.T) {
		srv, st := w9aOriginServer(t, "w9a-9-way4.db")
		w9aRequireAmendRoute(t, srv)
		w9aSeedPerson(t, st, "site-r", 1, "p-walker", "Wanda Walker")
		w9aSeedPerson(t, st, "site-r", 1, "p-mate", "Manny Mate")
		trip := w9aDegenerateTrip(t, st, "site-a", 1, "p-walker", []string{"p-mate"})

		// (a) a DESTINATION assertion is refused, with the check-in copy.
		page, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID:          {trip.TripID},
			w9aFieldDestKind:        {"controlled"},
			w9aFieldDestValue:       {"site-b"},
			w9aFieldDeadlineMinutes: {"240"},
		})
		if authored {
			t.Fatalf("W9a-9 (m8d — the PHANTOM AWAY-ENTRY): a destination amendment on a DEGENERATE gate check-in trip AUTHORED. "+
				"F4 §3.5: the record would now say a crew departed for a destination on a \"trip\" that was a walk through the gate, it would surface "+
				"in the away sections, and because destination may be CHANGED, NEVER CLEARED it is UNCORRECTABLE by this design's own rules. Page:\n%s", page)
		}
		if !w9aCheckInCopy.MatchString(w3PageText(page)) {
			t.Fatalf("W9a-9: the degenerate-trip refusal does not carry the check-in copy. F4 §3.5: \"This record is a gate check-in, not a trip … "+
				"A check-in is corrected on the visit side (re-check-in with the true time, or a ticked checkout — sweep row 10), not here.\" "+
				"Page text:\n%s", w3PageText(page))
		}

		// (b) a VEHICLE assertion is refused on the same trip.
		if _, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID: {trip.TripID}, w9aFieldVehicle: {"BUS 4 CORRECTED"},
		}); authored {
			t.Fatalf("W9a-9 (m8d): a VEHICLE amendment on a degenerate gate check-in trip AUTHORED — F4 §3.5 refuses Destination, VehicleEntered and DeadlineMinutes on this shape")
		}

		// (c) THE CONTROL, and it is the half that makes (a) and (b) mean
		// something: an OCCUPANT amendment on the very same degenerate trip
		// is ACCEPTED. Without it, a build that refused every amendment on
		// every degenerate trip would pass (a) and (b) and be wrong — F4
		// §3.5: "Occupant and driver amendments on degenerate trips remain
		// permitted … they touch no visit, no board and no away section".
		// ⟦D-80⟧ ⟦was: the form also carried confirm_unsighted=1⟧ p-mate has a
		// visit from this check-in's own arrival, so he is SIGHTED and the
		// record-only removal authors with nothing to confirm past.
		if _, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
			w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-mate"},
		}); !authored {
			t.Fatalf("W9a-9 CONTROL FAILED: an OCCUPANT amendment on a DEGENERATE trip was REFUSED. F4 §3.5 permits occupant and driver amendments " +
				"there in terms — they correct only who the check-in RECORDED. Without this arm, a build that refuses everything on degenerate trips " +
				"passes the two refusals above and is wrong")
		}
	})
}

// =========================================================================
// W9a-10 (T9) — the unsighted-removal warning
// =========================================================================

// TestW9a10UnsightedRemovalWarningAndTheCryWolfControl — W9a-10 (T9), as
// re-ruled by [[D-80]] (2026-09-05): §7's WARNING IS NOW A REFUSAL.
//
// James, verbatim: "they would not mark as never aboard they would just
// remove them from the list the same way as if they left" / "no one would be
// standing at a site without a vehicle as its -65F". The TRUE correction F4
// §7 chose a warning to protect — a person genuinely never aboard — is a
// situation that does not occur at this site; what the warning guarded
// against (removing a genuinely missing person from the ONE section that
// lists them) was the operation's only real effect. So the unsighted arm is
// REFUSED, with no confirm to pass, and the cry-wolf control SURVIVES as the
// discriminator: a SIGHTED person's removal is record-only (they are on a
// sheet by their visit) and still authors.
//
// ⟦was: "KILLER MUTANT: (m9) the ABSENT OR UNCONDITIONAL WARNING — either
// direction of the §7 hazard."⟧ KILLER MUTANTS since D-80: (m9) the CONFIRM
// BYPASS — a build that still honours confirm_unsighted, or refuses only
// when it is absent; (m9b) the BLANKET REFUSAL — a build that refuses SIGHTED
// removals too, refusing a record-only correction of someone demonstrably on
// a sheet.
//
// ⚠ THIS TEST DOES NOT CLOSE THE [[D-59]] REMOVAL GAP, and W9a-10's own text
// says so: the warning fires and a removal naming a MERGED-AWAY id still
// composes to a no-op. That gap is booked as a GATE OBLIGATION on W9a's gate,
// deliberately NOT as a criterion, and it must not be recorded as discharged
// by this test, by W9a-18, or by any composition criterion.
func TestW9a10UnsightedRemovalWarningAndTheCryWolfControl(t *testing.T) {
	srv, st := w9aOriginServer(t, "w9a-10.db")
	w9aRequireAmendRoute(t, srv)
	w9aSeedPerson(t, st, "site-r", 1, "p-drv10", "Dana Driver")
	w9aSeedPerson(t, st, "site-r", 1, "p-unsighted", "Una Sighted")
	w9aSeedPerson(t, st, "site-r", 1, "p-sighted", "Sam Seen")

	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-10", "p-drv10",
		[]string{"p-unsighted", "p-sighted"}, 240)
	// The trip arrives; p-sighted came back with it, p-unsighted did not.
	if _, err := domain.RecordExpectedArrivalExcluding(st, "site-b", 1, trip.TripID,
		[]string{"p-unsighted"}, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: expected arrival excluding: %v", err)
	}

	// --- the UNSIGHTED arm: REFUSED, and nothing to confirm past ----------
	// ⟦D-80⟧ ⟦was: "warn, and require the confirm" — the arm asserted the
	// warning named the person, rendered the confirm input, and that the
	// CONFIRMED re-send authored⟧
	page, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-unsighted"},
	})
	text := w3PageText(page)
	if authored {
		t.Fatalf("W9a-10 / D-80: removing an UNSIGHTED occupant AUTHORED. No return for Una Sighted has reached this post, so the manifest is the "+
			"only place she is accounted for — removing her takes her off the not-accounted-for section and onto no sheet at either post. "+
			"D-80 closed that door. Page:\n%s", page)
	}
	if !strings.Contains(text, "una sighted") {
		t.Fatalf("W9a-10: the refusal does not NAME the person being removed. The copy claims what THIS POST knows about a "+
			"NAMED human (L-1's honesty direction), not a generic caution. Page text:\n%s", text)
	}
	if !strings.Contains(text, strings.ToLower(unsightedRemovalRefusalSentence)) {
		t.Fatalf("W9a-10 / D-80: the refusal does not say WHY (%q) — the officer must be told the consequence the old warning named, now as the reason "+
			"for the refusal. Page text:\n%s", unsightedRemovalRefusalSentence, text)
	}
	if _, ok := w5dInputValueByName(page, w9aFieldConfirmUnsighted); ok {
		t.Fatalf("W9a-10 / D-80: the refusal page still renders an input named %q — a refusal has nothing to confirm PAST; a confirm affordance here is "+
			"the old warning wearing a REFUSED banner. ⟦was: the warning page HAD to render it⟧ Page:\n%s", w9aFieldConfirmUnsighted, page)
	}
	// (m9) THE CONFIRM BYPASS IS GONE: a stale form (or a bookmark of the old
	// warning page) still carrying confirm_unsighted=1 must be refused all
	// the same. ⟦was: "the confirmed removal of an unsighted occupant did NOT
	// author … F4 §7 chooses a WARNING, NOT A REFUSAL, on purpose"⟧
	if page3, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-unsighted"},
		w9aFieldConfirmUnsighted: {"1"},
	}); authored {
		t.Fatalf("W9a-10 / D-80 (m9 — the confirm bypass): confirm_unsighted=1 AUTHORED the removal of an UNSIGHTED occupant. The confirm was the "+
			"old warning's escape; D-80 made the refusal unconditional on sightedness. Page:\n%s", page3)
	}

	// --- the CRY-WOLF CONTROL, now the DISCRIMINATOR (m9b) -----------------
	// A SIGHTED person came back with the vehicle and has a visit: they are
	// on a sheet by that visit, so removing them from the manifest is
	// record-only, touches no muster category, and AUTHORS with no warning
	// and no refusal. Without this arm a build that refused EVERY removal
	// would pass the arm above and be wrong — it would refuse to record a
	// true correction about someone demonstrably elsewhere.
	page2, authored2 := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-sighted"},
	})
	if !authored2 {
		t.Fatalf("W9a-10 / D-80 (m9b — the BLANKET refusal): removing a SIGHTED occupant did not author. Sam Seen has a visit from this trip's "+
			"arrival; he is on the destination's sheet whatever the manifest says. Refusing this refuses a record-only correction. Page:\n%s", page2)
	}
	if _, ok := w5dInputValueByName(page2, w9aFieldConfirmUnsighted); ok {
		t.Fatalf("W9a-10: a SIGHTED occupant's removal rendered a confirm affordance. Page:\n%s", page2)
	}
	if strings.Contains(w3PageText(page2), strings.ToLower(unsightedRemovalRefusalSentence)) {
		t.Fatalf("W9a-10 / D-80: the D-80 refusal sentence appears on a SIGHTED removal's confirmation — cry-wolf on a life-safety surface. Page:\n%s", page2)
	}
}

// =========================================================================
// W9a-4 / W9a-5 / W9a-6 — the RENDERED halves
// =========================================================================

// TestW9a4LosingPostsConfirmationRendersTheAdverseSentence — W9a-4's render
// half: "the LOSING post's rendered confirmation shows the VF §5 adverse
// sentence, naming the winning post's display name and the effective value
// `drv-okafor` — ON A PAGE, not only in the API."
//
// KILLER MUTANT: (m13) the MISSING RENDERER — resolution is correct and
// nobody is told.
//
// The fixture is W9a-4's U row: the amendment authored at the LOSING post
// (site-a) carrying the LATEST wall-instant, against a peer amendment at
// site-b whose key wins. Site-b beats site-a on the first term of the
// comparator, so this pairing is the cross-site half of the locked table.
// ⟦SchemaVersion 11, D-92 — RE-RULED, not silently edited⟧ At 10 this
// fixture applied the peer's driver correction FIRST and then POSTed the
// local one; under fixed order the local one lost. Under D-92 that
// construction is a correction made IN KNOWLEDGE of the peer's (its `seen`
// names it) and it WINS — the reversed pin is
// TestW9a4CorrectionMadeInKnowledgeOfThePeersWinsAndCarriesNoAdverseSentence.
// The losing arm is the GENUINELY CONCURRENT case: the local officer POSTs
// first, the peer's correction is authored cut off (a wire envelope names
// nothing) and arrives afterwards, and the site_id tiebreak decides. The
// officer meets the loss on a re-GET of their own confirmation (read-time
// truth, VF §4.3 rule 2).
// ⟦was: the peer's amendment applied, then the POST; the page read once⟧
func TestW9a4LosingPostsConfirmationRendersTheAdverseSentence(t *testing.T) {
	srv, st := w9aOriginServer(t, "w9a-4-render.db")
	w9aRequireAmendRoute(t, srv)
	w9aSeedPerson(t, st, "site-r", 1, "drv-start", "Start Driver")
	w9aSeedPerson(t, st, "site-r", 1, "drv-okafor", "drv-okafor")
	w9aSeedPerson(t, st, "site-r", 1, "drv-vance", "drv-vance")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-4R", "drv-start", nil, 240)

	// The LOSER, authored HERE first — the peer has not seen it.
	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec, cookie := w2PostForm(t, srv, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldDriver: {"drv-vance"},
	})
	first, location := w3FollowRecorded(t, srv, rec, cookie)
	if w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch) == before {
		t.Fatalf("W9a-4 render half: the losing amendment did not author. A losing assertion is RECORDED and superseded, never refused ([[D-1]]). Page:\n%s", first)
	}
	if n, has := w5dNoticeCarrying(first, "superseded"); has {
		t.Fatalf("fixture (VF §4.3 rule 2): before the peer's correction arrives the local one is Effective, yet its confirmation renders %q — a status stamped at write", n)
	}

	// The WINNER: authored at the peer, cut off, and replicated in afterwards.
	// A hand-built wire envelope carries no `seen`, so the pair is concurrent
	// by construction and site-b's key wins the tie.
	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-okafor"),
	}.payload())
	l1CrossPostWinner(t, st, w3RecordedKey(t, location), w9aFieldDriver, "site-b")
	page := w2Get(t, srv, location, cookie).Body.String()
	notice, ok := w5dNoticeCarrying(page, "superseded")
	if !ok {
		t.Fatalf("W9a-4 (m13 — the missing renderer): the LOSING post's confirmation carries no adverse notice. VF §5's rendering 3 is owed to the "+
			"officer who just typed something that will never be displayed anywhere. Notices rendered: %v\nPage:\n%s", w5dNoticeTexts(page), page)
	}
	winner := w9aPeerDisplayName(srv, "site-b")
	if winner != "" && !strings.Contains(notice, strings.ToLower(winner)) {
		t.Fatalf("W9a-4: the adverse sentence does not name the WINNING post (%q). VF §5 derives the name from config, never from a hardcoded "+
			"template — the officer must know where to go and ask. Notice: %q", winner, notice)
	}
	if !strings.Contains(notice, "drv-okafor") {
		t.Fatalf("W9a-4: the adverse sentence does not carry the EFFECTIVE value %q — \"the record shows ⟨value⟩\" is the half that tells the officer "+
			"what IS true, not merely that they lost. Notice: %q", "drv-okafor", notice)
	}
}

// TestW9a4CorrectionMadeInKnowledgeOfThePeersWinsAndCarriesNoAdverseSentence
// — D-92's reversal of W9a-4's fixture as it stood at 10: the peer's driver
// correction is already held, and the local officer, seeing it, corrects it
// again. The local correction WINS at the LESSER key: the composed driver
// is the local value, Outcome(local) is Effective and Outcome(peer) is
// Superseded BY the local key, and the confirmation is rendering 2 on this
// surface — it says the correction now stands in place of the peer's, and
// carries no "superseded". ⟦was: at 10 this exact construction was W9a-4's
// LOSING fixture — "the peer's higher key wins"; D-28 Part B, reversed by
// D-92⟧ Kills m-D92a on the trip carrier.
func TestW9a4CorrectionMadeInKnowledgeOfThePeersWinsAndCarriesNoAdverseSentence(t *testing.T) {
	srv, st := w9aOriginServer(t, "w9a-4-render-causal.db")
	w9aRequireAmendRoute(t, srv)
	w9aSeedPerson(t, st, "site-r", 1, "drv-start", "Start Driver")
	w9aSeedPerson(t, st, "site-r", 1, "drv-okafor", "drv-okafor")
	w9aSeedPerson(t, st, "site-r", 1, "drv-vance", "drv-vance")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-4C", "drv-start", nil, 240)

	// The peer's EARLIER correction, replicated in and read on the screen.
	peer := w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-okafor"),
	}.payload())

	rec, cookie := w2PostForm(t, srv, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldDriver: {"drv-vance"},
	})
	page, location := w3FollowRecorded(t, srv, rec, cookie)
	local := w3RecordedKey(t, location)
	l1AssertCausalWinner(t, st, local, peer.Key(), w9aFieldDriver)

	composed, err := domain.EffectiveTrip(st, trip.TripID)
	if err != nil || composed == nil {
		t.Fatalf("EffectiveTrip: %v (%v)", err, composed)
	}
	if composed.Trip.DriverPersonID != "drv-vance" {
		t.Fatalf("D-92 (m-D92a): composed driver = %q, want the correction made in knowledge of the peer's, %q — %q is D-28 Part B's answer", composed.Trip.DriverPersonID, "drv-vance", "drv-okafor")
	}
	if n, has := w5dNoticeCarrying(page, "superseded"); has {
		t.Fatalf("D-92 (VF-m7′'s shape): the WINNING correction's confirmation renders the adverse register: %q — the cry-wolf defect", n)
	}
	notice, ok := w5dNoticeCarrying(page, "now stands")
	if !ok {
		t.Fatalf("D-92 / VF §5 rendering 2: the WINNING correction's confirmation does not say it now stands in place of the peer's. Notices: %v", w5dNoticeTexts(page))
	}
	if winner := w9aPeerDisplayName(srv, "site-b"); winner != "" && !strings.Contains(notice, strings.ToLower(winner)) {
		t.Fatalf("D-92 / VF §5 rendering 2: the notice does not name the superseded post %q (derived from config). Notice: %q", winner, notice)
	}
}

// TestW9a5DisjointEditsCarryNoSupersessionLanguage — W9a-5's render half.
//
// KILLER MUTANT: (m5b) UNCONDITIONAL SUPERSESSION LANGUAGE — the cry-wolf
// rule, VF attack 2.
//
// ⚠ THE POSITIVE CONTROL IS THIS TEST'S POINT. A "no supersession language"
// assertion is satisfied by a surface that can never say it at all. So the
// SAME fixture, on the SAME surface, first produces an amendment that MUST
// carry the adverse register; only then is the disjoint amendment's silence
// evidence.
func TestW9a5DisjointEditsCarryNoSupersessionLanguage(t *testing.T) {
	srv, st := w9aOriginServer(t, "w9a-5-render.db")
	w9aRequireAmendRoute(t, srv)
	for _, p := range []string{"p-alvarez", "p-brand", "p-chen", "p-delgado"} {
		w9aSeedPerson(t, st, "site-r", 1, p, p)
	}
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-5R", "p-alvarez",
		[]string{"p-brand", "p-chen"}, 240)

	// ⟦D-80, 2026-09-05⟧ THE FIXTURE MOVED FROM AN OPEN TRIP TO AN ARRIVED ONE.
	// ⟦was: the contested removal was posted on the OPEN trip, carrying
	// confirm_unsighted=1 past §7's warning⟧ D-80 closed the removal of an
	// UNSIGHTED occupant at the officer surface. T5 is about per-fact
	// resolution of SET edits and says nothing about the trip's state, so
	// the same contest is posed after the trip arrives: p-brand came in with
	// it and is SIGHTED, the removal is record-only and still authors, and
	// the peer's higher key still supersedes it. The confirm field went with
	// the warning it belonged to.
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: expected arrival: %v", err)
	}

	// --- POSITIVE CONTROL: a CONTESTED member DOES draw the language ----
	// ⟦SchemaVersion 11, D-92 — the CONTROL RE-RULED, not silently edited⟧
	// At 10 the peer's add was applied first and the removal POSTed after;
	// under D-92 that removal is made IN KNOWLEDGE of the add and WINS, so
	// it draws no supersession language and the control would fail for the
	// wrong reason. The contested pair is now GENUINELY CONCURRENT: the
	// removal is POSTed first, the peer's add is authored cut off (a wire
	// envelope names nothing) and arrives afterwards, and the peer's higher
	// key wins the tie — met on a re-GET of the confirmation (read-time
	// truth). ⟦was: the peer's add applied, then the POST; the page read once⟧
	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec, cookie := w2PostForm(t, srv, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-brand"},
	})
	first, location := w3FollowRecorded(t, srv, rec, cookie)
	if w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch) == before {
		t.Fatalf("W9a-5 render half CONTROL: the contested removal did not author. Page:\n%s", first)
	}
	if n, has := w5dNoticeCarrying(first, "superseded"); has {
		t.Fatalf("fixture (VF §4.3 rule 2): before the peer's add arrives the removal is Effective, yet its confirmation renders %q", n)
	}
	// The peer asserts `occupant/p-brand` = aboard at a winning key, cut off.
	w9aApplyNext(t, st, "site-b", 1, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, OccupantsAdd: []string{"p-brand"},
	}.payload())
	l1CrossPostWinner(t, st, w3RecordedKey(t, location), "occupant/p-brand", "site-b")
	contested := w2Get(t, srv, location, cookie).Body.String()
	if _, ok := w5dNoticeCarrying(contested, "superseded"); !ok {
		t.Fatalf("W9a-5 render half CONTROL FAILED: the CONTESTED member's removal (`occupant/p-brand`, superseded by the peer's higher key) draws NO "+
			"supersession language on this surface. The surface cannot say it at all, so the negative below would prove nothing. Notices: %v",
			w5dNoticeTexts(contested))
	}

	// --- the real assertion: a DISJOINT member draws NONE ---------------
	disjoint, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsAdd: {"p-delgado"},
	})
	if !authored {
		t.Fatalf("W9a-5 render half: the disjoint addition did not author. Page:\n%s", disjoint)
	}
	if n, ok := w5dNoticeCarrying(disjoint, "superseded"); ok {
		t.Fatalf("W9a-5 (m5b — unconditional supersession language): adding `p-delgado`, whom NOBODY else asserted anything about, drew supersession "+
			"language: %q. F4 §6: removing one occupant does NOT contest with adding another — different raw ids are different fact names, both "+
			"effective, no supersession language (the cry-wolf rule, VF attack 2)", n)
	}
}

// TestW9a6InvariantOverrideSentenceOnTheLosingRemoval — W9a-6's render half.
//
// KILLER MUTANT: (m6b) the FALSELY-REASSURING `Effective` — a bare
// "recorded" on a removal the structural union has just negated. §3.4 calls
// it worse than an adverse rendering, "because the officer walks away
// believing Y is off the record".
func TestW9a6InvariantOverrideSentenceOnTheLosingRemoval(t *testing.T) {
	srv, st := w9aDestinationServer(t, "w9a-6-render.db")
	w9aRequireAmendRoute(t, srv)
	w9aSeedPerson(t, st, "site-r", 1, "drv-first", "First Driver")
	w9aSeedPerson(t, st, "site-r", 1, "p-yates", "Yolanda Yates")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-6R", "drv-first", []string{"p-yates"}, 240)

	// ⟦W2 seam, 2026-10-03⟧ THE INTERLEAVE IS NOW A TRUE "NOBODY SEES BOTH".
	// ⟦was: the other post's driver change was applied HERE first, and this
	// post then removed p-yates⟧ W2 (c670a10) made rung 8 ask the EFFECTIVE
	// driver (the T-164 fix), so a post that HAS seen p-yates become driver
	// now refuses the removal legibly, before authoring — §3.4's local
	// refusal doing its job, pinned at the end of this test. The render half
	// under test needs the case the local rung CANNOT see: this post removes
	// p-yates while its record still says drv-first drives, and only then
	// does the other post's driver change arrive. The removal authored, won
	// its per-fact contest, and is negated by the structural driver union —
	// so the confirmation (rendered on the redirect, from the record as it
	// now stands) must say so.
	//
	// ⟦D-80, 2026-09-05⟧ the fixture is an ARRIVED trip (an unsighted
	// occupant's removal is closed at the officer surface): p-yates has a
	// visit, is SIGHTED, and this post's record-only removal still authors.
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: expected arrival: %v", err)
	}

	// This post removes p-yates as an occupant — and WINS that fact.
	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec, cookie := w2PostForm(t, srv, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-yates"},
	})
	if after := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch); after == before {
		t.Fatalf("W9a-6 render half: the removal did not author (%d): %s", rec.Code, rec.Body.String())
	}
	// The other post made p-yates the driver; it reaches this post only now.
	w9aApplyNext(t, st, "site-a", 2, w2Base+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("p-yates"),
	}.payload())
	page := w2NextOperatorPage(t, srv, rec, cookie)
	text := strings.ToLower(w3PageText(page))
	if !strings.Contains(text, "overridden") || !strings.Contains(text, "recorded driver") {
		t.Fatalf("W9a-6 (m6b — the falsely-reassuring Effective): the confirmation for a removal NEGATED BY THE STRUCTURAL UNION carries no "+
			"invariant-override sentence. §3.4 defines it: \"Your removal of ⟨name⟩ is overridden: ⟨name⟩ is the recorded driver — the record shows "+
			"them aboard.\" The per-fact resolution says this removal WON (no competing membership assertion), so a bare `Effective` here is worse "+
			"than an adverse rendering: the officer walks away believing they are off the record. Page text:\n%s", w3PageText(page))
	}
	if !strings.Contains(text, "yolanda yates") && !strings.Contains(text, "p-yates") {
		t.Fatalf("W9a-6: the invariant-override sentence does not NAME the person. Page text:\n%s", w3PageText(page))
	}
	// And the half W2 added: now that this post HAS seen p-yates become the
	// driver, a second removal is refused here, in words, and authors nothing.
	page, authored := w9aAmend(t, srv, st, w9aTripAmendPath, url.Values{
		w9aFieldTripID: {trip.TripID}, w9aFieldOccupantsRemove: {"p-yates"},
	})
	if authored || !strings.Contains(w3PageText(page), "is the recorded driver of this trip") {
		t.Fatalf("W9a-6: removing the driver this post can see was not refused legibly (authored=%v):\n%s", authored, w3PageText(page))
	}
}

// =========================================================================
// helpers used only by this file
// =========================================================================

// w9aRequireAmendRoute fails with a sentence naming the criterion when the
// authoring route does not exist, rather than letting the mux's answer be
// mistaken for a refusal. THIS IS THE PRE-CHUNK RED for every test in this
// file.
func w9aRequireAmendRoute(t *testing.T, srv *Server) {
	t.Helper()
	if !w9aRouteExists(t, srv, http.MethodPost, w9aTripAmendPath) {
		t.Fatalf("W9a: `POST %s` is NOT REGISTERED. F4 §3.5 builds the authoring surface with the full pre-append refusal ladder and §9.4 "+
			"classifies it EXEMT-with-written-reason on the TP §4.8 successor table. On the pre-chunk tree it does not exist, and THIS is the produced red",
			w9aTripAmendPath)
	}
}

// w9aPeerDisplayName is the configured display name of a peer site — the
// derivation VF §5 requires (never a hardcoded post name).
func w9aPeerDisplayName(srv *Server, siteID string) string {
	for _, d := range srv.Cfg.Destinations {
		if d.ID == siteID {
			return d.DisplayName
		}
	}
	return ""
}

// w9aOverdueAtDestination builds the DESTINATION post holding a site-a trip
// that is OPEN and OVERDUE with its alert LIVE — the `alerts_test.go`
// pattern, which is this house's own way of producing an overdue_live row.
func w9aOverdueAtDestination(t *testing.T) (*Server, *store.Store, domain.Trip) {
	t.Helper()
	srv, st := w9aDestinationServer(t, "w9a-overdue.db")
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	w9aSeedPerson(t, st, "site-r", 1, "p-drv-ov", "Olive Overdue")
	w9aSeedPerson(t, st, "site-r", 1, "p-pax-ov", "Percy Passenger")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "ICE TRUCK 7", "p-drv-ov", []string{"p-pax-ov"}, 1)
	envs, err := st.Events("site-a", 1, trip.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("w9a fixture: fetching the trip envelope: %v", err)
	}
	// The receipt anchor leaves ~80ms of countdown, and real monotonic time
	// runs it out: a HEALTHY countdown expiring LIVE — `overdue_live`, not
	// the partition-expired `overdue_unresolved` review case. This is
	// `alerts_test.go`'s own `dispatchLiveOverdueTrip` shape; an anchor
	// simply placed in the past produces the review case instead, which
	// F4 §4 also covers but which is the weaker of the two states.
	d.OnApplied(envs[0], time.Now().Add(-time.Minute+80*time.Millisecond), true)
	time.Sleep(150 * time.Millisecond)
	board, err := d.Evaluate()
	if err != nil {
		t.Fatalf("w9a fixture: deadline evaluate: %v", err)
	}
	var got deadline.Status
	for _, a := range board.Banner() {
		if a.TripID == trip.TripID {
			got = a.Status
		}
	}
	// F4 §4's second rendering covers `overdue_live` AND
	// `overdue_unresolved`; either satisfies "open and overdue".
	if got != deadline.StatusOverdueLive && got != deadline.StatusOverdueUnresolved {
		t.Fatalf("PRECONDITION (W9a-9 way 2/3): trip %s carries alert status %q, want %q (or %q). The state-aware refusal has no state to be aware of. Board: %+v",
			trip.TripID, got, deadline.StatusOverdueLive, deadline.StatusOverdueUnresolved, board.Banner())
	}
	return srv, st, trip
}
