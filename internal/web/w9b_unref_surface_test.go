// w9b_unref_surface_test.go — W9b-2: the surface never re-points, on an
// ENUMERATED surface set, and the confirmation is a READ.
//
// ⚠ W9b-2 IS `⟦NO PRE-CHUNK RED⟧` BY ITS OWN TEXT, and the reason is worth
// keeping in front of a reader: "the subject does not exist — there is no
// unref surface at all on the delivered tree, so the negative is true by
// construction and a clean green books work nobody did. Its ledger evidence
// is the three m15 plants and mutant m15b, never a clean pass."
//
// This file is written so that the negative can NEVER be the whole of a pass:
//
//   - every surface test asserts the route EXISTS before asserting anything
//     about what it does not offer, so the pre-chunk run reds at the route
//     rather than going green on an absent page;
//   - the negative is stated over a fixture that CONTAINS the thing a
//     re-point would offer. Trip Y is a second open, non-degenerate trip at
//     the same destination — the natural pick for any replacement selector —
//     so "no other trip id appears on these three surfaces" is a LITERAL
//     assertion against a value that exists in the world, not a pattern scan
//     hoping to recognise a `<select>` it has never seen;
//   - the structural half is filter-independent: the payload's key set is
//     read off the LOG and compared to the three-element literal, and no grep
//     can drift away from it.
//
// THE THREE SURFACES, enumerated because "anywhere in the flow" is not a
// filter anyone can implement [F4 §3.6, W9R2-FA2-9]:
//
//  1. the un-ref form                     GET  /arrivals/unref
//  2. the post-recompute confirmation     the 303's target
//  3. the ArrivalRefRetracted review row  GET  /trips
//
// KILLER MUTANTS: m15 — the auto-repick / re-point, or a surface offering the
// pick (killed on all three surfaces, "not on whichever one a grep happened to
// cover"); m15b — refusal-that-authors, and the unsolicited-arrival hole;
// m20 — the pre-append prediction, killed by the crossing case.
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w9bMisClickWorld builds the world every W9b-2 test starts from, at ONE post
// (the destination, where the mis-click happens).
//
// ⚠ THE WORLD IS STATED AS A PARAMETER, not inherited — W9b-2 says so in
// terms. It carries: trip X, open and past its deadline, mis-referenced by
// arrival A1; trip Y, a SECOND open non-degenerate trip to the same
// destination and the natural replacement pick; NO standing acknowledgment;
// and NOTHING else binding X. Each of those is asserted as a precondition
// below rather than assumed, because every one of them is load-bearing for
// some assertion in this file.
type w9bMisClickWorld struct {
	Srv   *Server
	Store *store.Store
	X     domain.Trip
	Y     domain.Trip
	A1    domain.Arrival
}

func w9bNewMisClickWorld(t *testing.T, name string) *w9bMisClickWorld {
	t.Helper()
	st := w3OpenRegistered(t, name)
	srv := New(w3ConfigSiteB(st.Path), st)
	w9bWireScanner(t, srv, st, "site-b")

	w9bSeedPerson(t, st, "site-a", 1, "drv-x", "Driver Ex")
	w9bSeedPerson(t, st, "site-a", 1, "occ-x", "Occupant Ex")
	w9bSeedPerson(t, st, "site-a", 1, "drv-y", "Driver Why")

	x := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "ICE TRUCK 7", "drv-x", []string{"occ-x"}, 240)
	// Y is the replacement a re-pointing surface would offer: a second OPEN,
	// non-degenerate, same-destination trip. A different vehicle, so it can
	// never be correlated onto X's key by accident.
	y := w9bDispatchOpen(t, st, "site-a", "site-b", 1, "VAN 9", "drv-y", nil, 240)

	a1, err := domain.RecordExpectedArrival(st, "site-b", 1, x.TripID, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: the mis-click: %v", err)
	}

	w := &w9bMisClickWorld{Srv: srv, Store: st, X: x, Y: y, A1: a1}
	w.assertPreconditions(t)
	return w
}

func (w *w9bMisClickWorld) assertPreconditions(t *testing.T) {
	t.Helper()
	xs, err := domain.TripStateByID(w.Store, w.X.TripID)
	if err != nil || xs == nil {
		t.Fatalf("w9b fixture: no trip_state for X (%s): %v", w.X.TripID, err)
	}
	if xs.State != domain.TripStateArrived {
		t.Fatalf("PRECONDITION: X (%s) is %q, want %q — the mis-click must have closed it", w.X.TripID, xs.State, domain.TripStateArrived)
	}
	ys, err := domain.TripStateByID(w.Store, w.Y.TripID)
	if err != nil || ys == nil || ys.State != domain.TripStateOpen {
		t.Fatalf("PRECONDITION: Y (%s) must be an OPEN trip — it is the replacement a re-pointing surface would offer, and the negative below is stated as its ABSENCE", w.Y.TripID)
	}
	if w.X.TripID == w.Y.TripID {
		t.Fatalf("w9b FIXTURE DEFECT: X and Y are the same trip")
	}
}

// =========================================================================
// W9b-2 — the negative, on EACH of the three named surfaces
// =========================================================================

// TestW9b2NoSurfaceOffersAReplacementTripSelection is the m15 negative on all
// three surfaces, in one fixture and one run.
//
// KILLER MUTANT: m15 — an un-ref that binds a "corrected" trip in the same act
// (I-2's forbidden fold choice smuggled in through the correction), or a
// surface offering the pick. F4 §3.6: the un-reference "never selects a
// replacement: there is no 'new trip' field, by construction". The three
// surfaces are asserted separately because rev 3's single unbounded negative
// ("no surface offers a replacement-trip selection anywhere in the flow") let
// the builder choose the filter, and the filter is what the ledger certified.
func TestW9b2NoSurfaceOffersAReplacementTripSelection(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-norepoint.db")

	if !w9bRouteExists(t, w.Srv, "GET", w9bUnrefPath) {
		t.Fatalf("W9b-2: `GET %s` is NOT REGISTERED. F4 §3.6 builds the un-ref form (fields: `arrival_id`, nothing else) as the first of the flow's THREE operator-facing surfaces. "+
			"On the pre-chunk tree it does not exist, and THIS is the produced red — W9b-2's negative is otherwise true by construction and books work nobody did", w9bUnrefPath)
	}

	// --- SURFACE 1: the un-ref form -----------------------------------
	form := w9bGet(t, w.Srv, w9bUnrefPath)
	w9bAssertNoRepointAffordance(t, "surface 1 — the un-ref form ("+w9bUnrefPath+")", form, w.Y.TripID)

	// --- SURFACE 2: the post-recompute confirmation --------------------
	loc, confirmation, code, authored := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if code != http.StatusSeeOther {
		t.Fatalf("W9b-2: `POST %s` on an arrival carrying a TripRef returned %d, want 303. F4 §3.6: the POST appends and returns 303 to a confirmation page that READS the recomputed state",
			w9bUnrefPath, code)
	}
	if !authored {
		t.Fatalf("W9b-2: `POST %s` returned 303 and authored NOTHING — a success-shaped response with no event is the silent-OK class", w9bUnrefPath)
	}
	w9bAssertNoRepointAffordance(t, "surface 2 — the confirmation ("+loc+")", confirmation, w.Y.TripID)

	// --- SURFACE 3: the ArrivalRefRetracted review-lane row ------------
	trips := w9bGet(t, w.Srv, w9bTripsPath)
	rows := w9bReviewRows(trips)
	if len(rows) == 0 {
		t.Fatalf("W9b-2 / W9b-5: after the un-reference, `GET %s` renders NO review-lane row at all, so surface 3 of the enumerated three does not exist and its negative is vacuous. "+
			"F4 §3.6: the retracted arrival's review-lane row is \"squarely in the flow, and the surface a form-only filter misses\"", w9bTripsPath)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, w.A1.ArrivalID) {
		t.Fatalf("W9b-2 / W9b-5: no review-lane row names the retracted arrival %s. Rows rendered:\n%s", w.A1.ArrivalID, joined)
	}
	w9bAssertNoRepointAffordance(t, "surface 3 — the review-lane row on "+w9bTripsPath, joined, w.Y.TripID)

	// --- THE SAME NEGATIVE, AT THE HANDLER ------------------------------
	// m15's other half is not a rendered `<select>` at all: it is "a handler
	// accepting a `trip_id` and binding it". Posting one must change nothing
	// about Y, and must not appear on the wire.
	w2 := w9bNewMisClickWorld(t, "w9b-norepoint-handler.db")
	yStateBefore, err := domain.TripStateByID(w2.Store, w2.Y.TripID)
	if err != nil || yStateBefore == nil {
		t.Fatalf("w9b fixture: Y has no trip_state: %v", err)
	}
	_, _, _, _ = w9bUnref(t, w2.Srv, w2.Store, url.Values{
		w9bFieldArrival: {w2.A1.ArrivalID},
		"trip_id":       {w2.Y.TripID},
		"replacement":   {w2.Y.TripID},
	})
	yStateAfter, err := domain.TripStateByID(w2.Store, w2.Y.TripID)
	if err != nil || yStateAfter == nil {
		t.Fatalf("w9b: Y lost its trip_state row: %v", err)
	}
	if yStateAfter.State != domain.TripStateOpen {
		t.Fatalf("W9b-2 (m15 — the auto-repick): posting a `trip_id`/`replacement` alongside the un-reference moved trip Y (%s) from %q to %q. "+
			"F4 §3.6: the un-reference names ONE arrival and NOTHING else; a re-point smuggled through the correction is I-2's forbidden fold choice wearing a form field",
			w2.Y.TripID, domain.TripStateOpen, yStateAfter.State)
	}
	if yStateAfter.ArrivalID != nil {
		t.Fatalf("W9b-2 (m15 — the auto-repick): trip Y (%s) is now bound to arrival %q after an un-reference that mentioned it. Y was never arrived at",
			w2.Y.TripID, *yStateAfter.ArrivalID)
	}
}

// w9bAssertNoRepointAffordance is the m15 negative, applied identically to
// each of the three surfaces so none can be covered by a filter the others are
// not.
//
// It is stated in two independent ways, because either alone is escapable:
//
//	(a) NO FORM CONTROL names a trip. A `<select name="trip_id">` is m15's
//	    canonical shape and this catches it whatever its options.
//	(b) THE OTHER OPEN TRIP'S ID IS ABSENT. This is the literal, fixture-backed
//	    half: a selector populated from the open-trip set renders Y's id
//	    whatever it calls its field, and a page that offers no pick cannot
//	    contain it. The REPUDIATED trip's id is of course allowed — the
//	    confirmation and the review row must both name it.
func w9bAssertNoRepointAffordance(t *testing.T, surface, page, otherTripID string) {
	t.Helper()
	for _, name := range w9bControlNames(page) {
		if strings.Contains(strings.ToLower(name), "trip") {
			t.Fatalf("W9b-2 (m15) on %s: the surface carries a form control named %q. F4 §3.6: the un-reference \"never selects a replacement — there is no 'new trip' field, by construction\"",
				surface, name)
		}
	}
	if strings.Contains(w2StripNonMarkup(page), otherTripID) {
		t.Fatalf("W9b-2 (m15) on %s: the OTHER open trip's id (%s) appears on this surface. "+
			"It is not the repudiated trip and there is no legitimate reason for the un-ref flow to name it; a replacement selector populated from the open-trip set renders exactly this, "+
			"whatever it calls its field",
			surface, otherTripID)
	}
}

// =========================================================================
// W9b-2 — the filter-independent structural assertion
// =========================================================================

// TestW9b2RetractionPayloadHasExactlyThreeFieldsAndNoTripField is the half
// "no grep can drift away from": the payload has exactly three fields —
// `type`, `arrival_id`, `recorded_by` — and no trip field of any kind, so a
// re-point is not merely absent from the surfaces, it is UNREPRESENTABLE on
// the wire.
//
// Read off the replicated LOG, not off a Go struct, because the wire is what
// a peer folds and what gate B-2 freezes.
func TestW9b2RetractionPayloadHasExactlyThreeFieldsAndNoTripField(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-payload.db")
	if !w9bRouteExists(t, w.Srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-2: `POST %s` is NOT REGISTERED — the payload this criterion pins cannot be authored. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}

	before := w2Watermark(t, w.Store, "site-b", 1)
	_, _, code, authored := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if !authored {
		t.Fatalf("W9b-2: the un-reference authored nothing (status %d)", code)
	}

	envs := w2EventsFrom(t, w.Store, "site-b", 1, before)
	var retractions []store.Envelope
	for _, e := range envs {
		if w2PayloadType(t, e) == w9bEvtRefRetracted {
			retractions = append(retractions, e)
		}
	}
	if len(retractions) != 1 {
		var got []string
		for _, e := range envs {
			got = append(got, w2PayloadType(t, e))
		}
		t.Fatalf("W9b-2: the un-reference appended %d `%s` events, want exactly 1. Types appended: %v",
			len(retractions), w9bEvtRefRetracted, got)
	}

	var m map[string]any
	if err := json.Unmarshal(retractions[0].Payload, &m); err != nil {
		t.Fatalf("W9b-2: the retraction payload is not a JSON object: %v (%s)", err, retractions[0].Payload)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != strings.Join(w9bPayloadKeys, ",") {
		t.Fatalf("W9b-2: the `%s` payload's key set is %v, want EXACTLY %v.\nwire: %s\n"+
			"F4 §3.6: the event \"names the arrival whose reference is withdrawn and NOTHING else — no replacement trip, ever\". "+
			"These three literals are gate B-2 material: they land inside hashed bytes",
			w9bEvtRefRetracted, keys, w9bPayloadKeys, retractions[0].Payload)
	}
	for _, k := range keys {
		if strings.Contains(strings.ToLower(k), "trip") {
			t.Fatalf("W9b-2: the `%s` payload carries the field %q. A trip field of ANY kind makes the re-point representable on the wire, and W9b-2's claim that it is not becomes false — "+
				"including the variant that adds one \"for the review lane's convenience\"", w9bEvtRefRetracted, k)
		}
	}
	if got, _ := m[w9bKeyArrivalID].(string); got != w.A1.ArrivalID {
		t.Fatalf("W9b-2: the retraction names arrival %q, want the mis-clicked arrival %q", got, w.A1.ArrivalID)
	}
}

// =========================================================================
// W9b-2 — success is a POST-RECOMPUTE READ, never a pre-append prediction
// =========================================================================

// TestW9b2ConfirmationIsReadFromTheRecomputedState is the read half in the
// fixture's stated world (no standing ack, nothing else binding X): follow the
// 303 and read the rendering off X's recomputed `trip_state` and the arrival's
// recomputed `arrival_state`.
//
// The assertion is made in BOTH directions — the reopened copy is present AND
// the still-closed copy is absent — because a page that renders every possible
// sentence contains the right one too.
func TestW9b2ConfirmationIsReadFromTheRecomputedState(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-confirm-read.db")
	if !w9bRouteExists(t, w.Srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-2: `POST %s` is NOT REGISTERED — there is no confirmation to read. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}

	loc, page, code, _ := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if code != http.StatusSeeOther {
		t.Fatalf("W9b-2: POST returned %d, want 303 — the confirmation is a page reached by redirect and re-readable, not a body composed by the POST (SEAM W9B-S-CONFIRM)", code)
	}

	// The recomputed state, read the same way the page must read it.
	xs, err := domain.TripStateByID(w.Store, w.X.TripID)
	if err != nil || xs == nil {
		t.Fatalf("w9b: no trip_state for X: %v", err)
	}
	if xs.State != domain.TripStateOpen {
		t.Fatalf("W9b-2 PRECONDITION: after the un-reference X (%s) reads %q, want %q — this fixture's world has nothing else binding it, so the confirmation under test is the REOPENED one",
			w.X.TripID, xs.State, domain.TripStateOpen)
	}

	body := w2StripNonMarkup(page)
	if !strings.Contains(body, w.X.TripID) {
		t.Fatalf("W9b-2: the confirmation at %s does not name the repudiated trip %s.\npage:\n%s", loc, w.X.TripID, body)
	}
	if !w9bSaysOpen(body) {
		t.Fatalf("W9b-2: the confirmation at %s does not say trip %s is OPEN, and the recomputed `trip_state` says it is.\npage:\n%s\n"+
			"F4 §3.6's first rendering: \"Trip ⟨X⟩ is OPEN — its deadline expectation is live again at both posts, and its alert is live now.\" (SEAM W9B-S-COPY)",
			loc, w.X.TripID, body)
	}
	if w9bSaysStillArrived(body) {
		t.Fatalf("W9b-2: the confirmation at %s renders the STILL-ARRIVED copy on a trip the recompute reopened. A page that renders both copies unconditionally contains the right one too, "+
			"and would pass a one-sided check while telling every officer both things.\npage:\n%s", loc, body)
	}
}

// TestW9b2ConfirmationSurvivesTheCROSSINGCase is T12c's second half at the
// surface, and it is the only place m20 can die.
//
// KILLER MUTANT: m20 — the pre-append prediction: the confirmation string
// composed BEFORE the append and its recompute. It is green wherever
// prediction and outcome agree, and it is STRUCTURALLY INCAPABLE of being
// right here — the peer's arrival does not exist when a prediction would be
// composed, so no prediction can name it.
//
// The fixture: the officer un-refs at the destination; the peer's arrival U —
// carrying X's correlation key and NO reference — is delivered AFTERWARDS; the
// confirmation page is fetched AGAIN. Being a read at render time, it must now
// say X is still ARRIVED and name U.
func TestW9b2ConfirmationSurvivesTheCROSSINGCase(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-crossing.db")
	if !w9bRouteExists(t, w.Srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-2: `POST %s` is NOT REGISTERED — the crossing case has no confirmation to re-read. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}
	w9bSeedPerson(t, w.Store, "site-a", 1, "pax-cross", "Pax Crossing")

	loc, page, code, _ := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if code != http.StatusSeeOther || loc == "" {
		t.Fatalf("W9b-2: POST returned %d with Location %q, want a 303 to a re-readable confirmation (SEAM W9B-S-CONFIRM)", code, loc)
	}
	if !w9bSaysOpen(w2StripNonMarkup(page)) {
		t.Fatalf("W9b-2 PRECONDITION: before U crosses, the confirmation must read OPEN — otherwise the change this test is about cannot be observed.\npage:\n%s", w2StripNonMarkup(page))
	}

	// U crosses the wire: authored at the PEER, delivered now, carrying X's
	// correlation key and no reference at all.
	uID := "arr-U-cross"
	w9bApplyNext(t, w.Store, "site-a", 1, w2Base+w9bMinute, domain.TripArrival{
		Type: "trip_arrival", ArrivalID: uID,
		VehicleEntered: w9bStr("ICE TRUCK 7"), OriginSite: w9bStr("site-a"),
		Visits:     []domain.ArrivalVisit{{VisitID: uID + "-v0", PersonID: "pax-cross"}},
		RecordedBy: w9bStr("Ice Road"),
	})

	xs, err := domain.TripStateByID(w.Store, w.X.TripID)
	if err != nil || xs == nil {
		t.Fatalf("w9b: no trip_state for X: %v", err)
	}
	if xs.State != domain.TripStateArrived || xs.ArrivalID == nil || *xs.ArrivalID != uID {
		t.Fatalf("W9b-2 PRECONDITION (the crossing world): after U crossed, X (%s) reads %q bound to %v, want %q bound to %q — Pass 2 binds at exactly one candidate",
			w.X.TripID, xs.State, xs.ArrivalID, domain.TripStateArrived, uID)
	}

	after := w2StripNonMarkup(w9bGet(t, w.Srv, loc))
	if !w9bSaysStillArrived(after) {
		t.Fatalf("W9b-2 (m20 — the pre-append prediction): re-read after U crossed, the confirmation at %s does not say trip %s is STILL ARRIVED, and the recomputed state says it is.\npage:\n%s\n"+
			"F4 §3.6: all three strings are \"functions of rows that exist AFTER the append and its recompute, so the crossing case resolves itself: whatever the peer's arrival did, the page reports it. "+
			"There is no world in which the officer is told an alert returned and it did not\"",
			loc, w.X.TripID, after)
	}
	if !strings.Contains(after, uID) {
		t.Fatalf("W9b-2: the re-read confirmation at %s does not NAME the binder %s.\npage:\n%s\n"+
			"F4 §3.6's second rendering names ⟨U⟩ — an officer told \"still arrived\" without being told what accounts for it cannot act on the sentence", loc, uID, after)
	}
	if !w9bSaysAutomatic(after) {
		t.Fatalf("W9b-2: the re-read confirmation names %s but does not declare the binding AUTOMATIC, with no officer selection.\npage:\n%s\n"+
			"[[D-58]] cost 3: under an auto-correlation the officer has said \"this trip did not arrive\" and the fold has answered \"yes it did\" with NO human selection, "+
			"and the copy must not disguise that as another officer's judgement (SEAM W9B-S-COPY)", uID, after)
	}
}

// =========================================================================
// W9b-2 — the refusal halves
// =========================================================================

// TestW9b2UnrefRefusalsAuthorNothing is both refusal halves.
//
// KILLER MUTANT: m15b — refusal-that-authors, and the unsolicited-arrival
// hole. "Nothing authored" is asserted on the LOG WATERMARK, never on a status
// code: a handler that appends and then renders a refusal is exactly the
// mutant, and it returns 409 too.
func TestW9b2UnrefRefusalsAuthorNothing(t *testing.T) {
	w := w9bNewMisClickWorld(t, "w9b-refusals.db")
	if !w9bRouteExists(t, w.Srv, "POST", w9bUnrefPath) {
		t.Fatalf("W9b-2: `POST %s` is NOT REGISTERED — neither refusal can be produced. On the pre-chunk tree that is the produced red", w9bUnrefPath)
	}
	w9bSeedPerson(t, w.Store, "site-a", 1, "pax-uns", "Pax Unsolicited")

	// --- half 1: an UNSOLICITED (fold-correlated) arrival ---------------
	// Y is open and matches on (vehicle, origin), so this arrival is bound by
	// Pass 2 — a FOLD-DERIVED binding, which §5 rule 4 forbids touching.
	// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ ⟦was: "uns, err := domain.RecordUnsolicitedArrival(w.Store, "site-b", 1, "VAN 9", "site-a", []string{"pax-uns"}, w9bStr("Pad Site Control"), 0, "default")"⟧
	uns, err := domain.RecordUnsolicitedArrival(w.Store, "site-b", 1, w7SeedVehicleID(t, w.Store, "site-b", "VAN 9"), "site-a", []string{"pax-uns"}, w9bStr("Pad Site Control"), 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: unsolicited arrival: %v", err)
	}
	if got := w9bDispositionOf(t, w.Store, uns.ArrivalID); got != domain.ArrivalCorrelated {
		t.Fatalf("PRECONDITION: the unsolicited arrival %s is %q, want %q — the refusal under test is specifically the FOLD-DERIVED binding, and an uncorrelated arrival would not exercise it",
			uns.ArrivalID, got, domain.ArrivalCorrelated)
	}

	_, page, code, authored := w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {uns.ArrivalID}})
	if code != http.StatusConflict {
		t.Fatalf("W9b-2: un-reffing the unsolicited arrival %s returned %d, want 409. F4 §3.6 refuses when `TripRef` is nil: \"an unsolicited arrival's binding is fold-derived — correcting THAT is the review lane, and §5 rule 4 forbids touching it\"",
			uns.ArrivalID, code)
	}
	if authored {
		t.Fatalf("W9b-2 (m15b — refusal-that-authors): the 409 on the unsolicited arrival %s MOVED THE WATERMARK. A refusal that appends is a refusal in name only, and the event replicates", uns.ArrivalID)
	}
	if !w9bMentionsReviewLane(page) {
		t.Fatalf("W9b-2: the 409 on the unsolicited arrival does not name the review lane.\npage:\n%s\n"+
			"F4 §3.6 routes the officer there in terms — a refusal that closes the door without saying which door is open is the dead end this project has now found repeatedly (SEAM W9B-S-COPY)",
			w2StripNonMarkup(page))
	}

	// --- half 2: a SECOND un-reference of the same arrival ---------------
	if _, _, code, authored = w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}}); code != http.StatusSeeOther || !authored {
		t.Fatalf("W9b-2 PRECONDITION: the FIRST un-reference of %s returned %d (authored=%v), want 303 with an event — the second-unref refusal is only meaningful after a first one succeeded",
			w.A1.ArrivalID, code, authored)
	}
	_, page, code, authored = w9bUnref(t, w.Srv, w.Store, url.Values{w9bFieldArrival: {w.A1.ArrivalID}})
	if code != http.StatusConflict {
		t.Fatalf("W9b-2: the SECOND un-reference of %s returned %d, want 409. F4 §3.6's third refusal: \"reference already retracted\"", w.A1.ArrivalID, code)
	}
	if authored {
		t.Fatalf("W9b-2 (m15b — refusal-that-authors): the second un-reference of %s MOVED THE WATERMARK. A duplicate retraction on the wire is not harmless: it is a second row in a dump-participating "+
			"INSERT-only table, and the two posts' dumps stop agreeing", w.A1.ArrivalID)
	}
}

// --- SEAM W9B-S-COPY predicates ------------------------------------------
//
// Each is deliberately a small token test rather than a sentence match: the
// criteria pin WHAT must be said, and [[D-62]] routes HOW the three W9
// confirmations word it to W9's build critique.

func w9bSaysOpen(body string) bool {
	u := strings.ToUpper(body)
	return strings.Contains(u, "OPEN")
}

func w9bSaysStillArrived(body string) bool {
	u := strings.ToUpper(body)
	return strings.Contains(u, "STILL ARRIVED") || strings.Contains(u, "STILL <STRONG>ARRIVED") ||
		(strings.Contains(u, "STILL") && strings.Contains(u, "ARRIVED"))
}

func w9bSaysAutomatic(body string) bool {
	u := strings.ToUpper(body)
	return strings.Contains(u, "AUTOMATIC") && (strings.Contains(u, "NO OFFICER SELECTION") || strings.Contains(u, "WITHOUT AN OFFICER") || strings.Contains(u, "NO OFFICER"))
}

func w9bMentionsReviewLane(page string) bool {
	u := strings.ToUpper(w2StripNonMarkup(page))
	return strings.Contains(u, "REVIEW")
}

// w9bDispositionOf reads one arrival's derived disposition.
func w9bDispositionOf(t *testing.T, st *store.Store, arrivalID string) string {
	t.Helper()
	rows, err := domain.ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ArrivalID == arrivalID {
			return r.Disposition
		}
	}
	t.Fatalf("w9b: no arrival_state row for %s", arrivalID)
	return ""
}
