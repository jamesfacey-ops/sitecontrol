// gate_orientation_doors_test.go — closing the two gaps
// reports/witness/CROSSSITE-WARNING.md declared for the D-88 cross-site
// orientation warning (orientation-doors lane, 2026-09-11):
//
// GAP 1: /gate/resume and the legacy check-in body held an un-oriented
// occupant out with no pre-commit surfacing and no way to tick — fixed by
// extending the SAME authoring path (completeOrientationsFor, gate.go) to
// both doors, scoped (scopeToKnown / completeOrientationsForTrip) so a
// posted id is never trusted beyond "actually aboard this vehicle/trip".
//
// GAP 2: a row that went stale between the accumulate step that showed the
// warning and the final commit silently dropped the tick — worse, an
// UNCAUGHT case could author an orientation for the WRONG person if the row
// drifted to a different known person who also needed one.
// AddOrientationFor/orient_target (gate.go, gate.html) now catch both
// shapes, and completeTickedOrientations refuses to author for a mismatch,
// surfacing it BY NAME (staletick.go) rather than leaving the officer
// believing the tick took effect.
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// staleOrientTickNoticeText is the text of the stale-tick notice(s) on a
// gate page, tags stripped — the gap-2 counterpart of notEnteredNoticeText.
func staleOrientTickNoticeText(page string) string {
	var out []string
	rest := page
	for {
		i := strings.Index(rest, `ORIENTATION NOT RECORDED`)
		if i < 0 {
			break
		}
		rest = rest[i:]
		j := strings.Index(rest, "</div>")
		if j < 0 {
			break
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
	return strings.Join(out, "\n")
}

// --- GAP 1a: /gate/resume ---------------------------------------------------

// TestResumeDoorWarningNamesThisPostAndDefaultsUnticked: GET /gate, with an
// incomplete check-in aboard an un-oriented occupant, shows the SAME
// cross-site warning wording under "Incomplete check-ins", naming this
// post, with an UNTICKED checkbox keyed to the person's id.
func TestResumeDoorWarningNamesThisPostAndDefaultsUnticked(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-RD1"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.orla.PersonID, w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if inc, err := domain.IncompleteCheckIns(w.st, "site-b"); err != nil || len(inc) != 1 {
		t.Fatalf("precondition: half-authored check-in not listed as incomplete (%d, %v)", len(inc), err)
	}
	// ⟦W3 F-L03-R3-2 ruling, 2026-10-03⟧ logged in: an anonymous render names
	// nobody's orientation status any more (see the anonymous half below).
	page := w3Get(t, w.srv, "/gate/vehicle")
	if !strings.Contains(page, trip.TripID) {
		t.Fatalf("the incomplete check-in for %s is not on /gate/vehicle at all", trip.TripID)
	}
	if anon := scriptRe.ReplaceAllString(orientBlockGet(t, w.srv, "/gate/vehicle"), ""); strings.Contains(anon, "NOT ORIENTED HERE") {
		t.Error("the anonymous /gate/vehicle names an occupant's orientation status")
	}
	warning := crossSiteWarningText(page)
	if warning == "" {
		t.Fatalf("no cross-site orientation warning rendered under Incomplete check-ins for Uma Unoriented.\npage: %s", page)
	}
	for _, want := range []string{
		"NOT ORIENTED HERE",
		"Uma Unoriented has not completed Pad Site Control's orientation",
		"there is no override",
		`name="add_orientation_person" value="` + w.uma.PersonID + `"`,
	} {
		if !strings.Contains(warning, want) {
			t.Errorf("the resume door's warning lacks %q.\nwarning: %s", want, warning)
		}
	}
	if strings.Contains(warning, `value="`+w.uma.PersonID+`" checked>`) {
		t.Errorf("the resume door's checkbox is PRE-TICKED — it must default unticked.\nwarning: %s", warning)
	}
	// Orla (already holds the orientation) gets no warning.
	if strings.Contains(page, "Orla Oriented has not completed") {
		t.Errorf("a warning was rendered for Orla Oriented, who already holds site-b's orientation")
	}
}

// TestResumeDoorTickedAuthorsOrientationEntersAndClearsHeldOut: ticking the
// resume door's checkbox authors THIS post's orientation for the named
// person (the same edge as POST /orientation/complete), and the resumed
// arrival then enters them normally instead of holding them out.
func TestResumeDoorTickedAuthorsOrientationEntersAndClearsHeldOut(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-RD2"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, w.srv, "/gate/resume", url.Values{
		"trip_id": {trip.TripID}, "time_source": {"default"},
		"add_orientation_person": {w.uma.PersonID},
	})
	if rec.Code != 303 {
		t.Fatalf("POST /gate/resume (ticked) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("a TICKED resume still redirected with a hold-out: %q", loc)
	}
	held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held == nil {
		t.Fatalf("site-b's orientation was NOT recorded for Uma despite the resume-door tick")
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.uma.PersonID] {
		t.Fatalf("Uma has no open visit after a ticked resume")
	}
	heldOut, err := domain.HeldOutAt(w.st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heldOut {
		if h.PersonID == w.uma.PersonID {
			t.Fatalf("Uma is still on the held-out list after a ticked resume: %+v", heldOut)
		}
	}
}

// TestResumeDoorScopesTickToThisTripsOccupants: a posted
// add_orientation_person id that does NOT belong to this trip's occupants
// is ignored — completeOrientationsForTrip never trusts a posted id beyond
// "actually aboard this vehicle". Nita is a real, real person who also
// lacks site-b's orientation, but she is not on this trip.
func TestResumeDoorScopesTickToThisTripsOccupants(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	nita := orientBlockPerson(t, w.st, "Nita NotOnThisTrip")
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-RD3"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, w.srv, "/gate/resume", url.Values{
		"trip_id": {trip.TripID}, "time_source": {"default"},
		"add_orientation_person": {nita.PersonID}, // not aboard this trip
	})
	if rec.Code != 303 {
		t.Fatalf("POST /gate/resume = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, nita.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Nita's orientation was recorded despite not being aboard this trip: held=%v err=%v", held, err)
	}
	// Uma, genuinely aboard but NOT ticked in this request, is still held
	// out exactly as before — the scoping did not accidentally grant her
	// the tick either.
	if held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Uma's orientation was recorded despite never being ticked: held=%v err=%v", held, err)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if open[w.uma.PersonID] {
		t.Fatalf("Uma entered despite lacking orientation and never being ticked")
	}
}

// TestResumeDoorRoadPostShowsNoWarning: D-88's road post has no orientation
// concept, so an incomplete check-in's occupants there must never trigger
// the resume door's warning, whatever the world's data — this is case (d),
// extended to this door.
func TestResumeDoorRoadPostShowsNoWarning(t *testing.T) {
	w := newOrientBlockWorld(t, false)
	if w.srv.Cfg.HasOrientation() {
		t.Fatal("precondition: this world must be a road post")
	}
	trip, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID:         w7SeedVehicleID(t, w.st, "site-b", "TRK-RD4"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	page := orientBlockGet(t, w.srv, "/gate/vehicle")
	if !strings.Contains(page, trip.TripID) {
		t.Fatalf("the incomplete check-in is not on /gate/vehicle at all")
	}
	if warning := crossSiteWarningText(page); warning != "" {
		t.Errorf("a road post rendered a cross-site orientation warning under Incomplete check-ins: %s", warning)
	}
	if strings.Contains(page, "add_orientation_person") {
		t.Errorf("a road post's page mentions add_orientation_person at all")
	}
	if strings.Contains(page, "NOT ORIENTED HERE") {
		t.Errorf("a road post's page mentions orientation by name")
	}
}

// --- GAP 1b: the legacy check-in body --------------------------------------

// TestLegacyBodyTickedAuthorsOrientationAndEntersPerson: the legacy
// driver_kind/occupant_kind body — kept live for a stale pre-redesign
// tab — can now also carry add_orientation_person, and a ticked id gets the
// SAME authoring as the live form's tick.
func TestLegacyBodyTickedAuthorsOrientationAndEntersPerson(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	rec := postForm(t, w.srv, "/gate/checkin", url.Values{
		"time_source":            {"default"},
		"vehicle":                {"TRK-LG1"},
		"driver_kind":            {"existing"},
		"driver_value":           {w.driver.PersonID},
		"occupant_kind":          {"existing"},
		"occupant_value":         {w.uma.PersonID},
		"add_orientation_person": {w.uma.PersonID},
	})
	if rec.Code != 303 {
		t.Fatalf("legacy POST /gate/checkin (ticked) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held == nil {
		t.Fatalf("site-b's orientation was NOT recorded for Uma despite the legacy body's tick")
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.uma.PersonID] {
		t.Fatalf("Uma has no open visit after a ticked legacy check-in")
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, notEnteredQueryKey+"=") {
		t.Fatalf("a ticked legacy check-in still redirected with a hold-out: %q", loc)
	}
}

// TestLegacyBodyScopesTickToVehicleOccupants: a posted
// add_orientation_person id naming somebody NOT on this vehicle's
// driver+occupants is ignored, same guarantee as the resume door.
func TestLegacyBodyScopesTickToVehicleOccupants(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	nita := orientBlockPerson(t, w.st, "Nita NotOnThisVehicle")
	rec := postForm(t, w.srv, "/gate/checkin", url.Values{
		"time_source":            {"default"},
		"vehicle":                {"TRK-LG2"},
		"driver_kind":            {"existing"},
		"driver_value":           {w.driver.PersonID},
		"occupant_kind":          {"existing"},
		"occupant_value":         {w.uma.PersonID},
		"add_orientation_person": {nita.PersonID}, // not on this vehicle
	})
	if rec.Code != 303 {
		t.Fatalf("legacy POST /gate/checkin = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, nita.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Nita's orientation was recorded despite not being on this vehicle: held=%v err=%v", held, err)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if open[w.uma.PersonID] {
		t.Fatalf("Uma entered despite lacking orientation and never being ticked")
	}
}

// TestLegacyBodyTickOfOneOccupantNeverEntersAnUntickedSecondOne is the
// legacy door's own instance of FALSIFIABILITY-2.md's M-A harder case:
// every existing fixture in this file (and the cross-site warning tests in
// gate_crosssite_warning_test.go) puts
// exactly ONE un-held occupant on the vehicle. This door's tick mechanism
// (scopeToKnown against an EXPLICIT posted add_orientation_person id list —
// structurally different from the live/accumulate form's per-row boolean
// flag, resolveTickedOrientations) has no "any tick" signal to collapse in
// the first place, but the property is worth measuring rather than assumed:
// two un-held occupants aboard, add_orientation_person names only one.
func TestLegacyBodyTickOfOneOccupantNeverEntersAnUntickedSecondOne(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	vic := orientBlockPerson(t, w.st, "Vic Ignored") // unoriented, second occupant
	rec := postForm(t, w.srv, "/gate/checkin", url.Values{
		"time_source":            {"default"},
		"vehicle":                {"TRK-LG3"},
		"driver_kind":            {"existing"},
		"driver_value":           {w.driver.PersonID},
		"occupant_kind":          {"existing", "existing"},
		"occupant_value":         {w.uma.PersonID, vic.PersonID},
		"add_orientation_person": {w.uma.PersonID}, // Vic deliberately NOT named
	})
	if rec.Code != 303 {
		t.Fatalf("legacy POST /gate/checkin = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, w.uma.PersonID, "site-b"); err != nil || held == nil {
		t.Fatalf("Uma's orientation was not recorded despite being named: held=%v err=%v", held, err)
	}
	if held, err := domain.OrientationHeld(w.st, vic.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("BROKEN: Vic's orientation was recorded despite never being named in add_orientation_person: held=%v err=%v", held, err)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.uma.PersonID] {
		t.Fatalf("Uma (named) has no open visit after the legacy tick")
	}
	if open[vic.PersonID] {
		t.Fatalf("BROKEN: Vic (not named) has an open visit — the tick spilled onto an un-named occupant")
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, notEnteredQueryKey+"="+vic.PersonID) {
		t.Fatalf("redirect %q does not carry Vic as held out", loc)
	}
}

// --- GAP 2: a row that goes stale between accumulate and commit -----------

// TestStaleOrientationTickDoesNotAttributeToTheWrongPersonAndNotifies is the
// sharp case gap 2 names: the officer ticks "add this post's orientation"
// for a row naming Stan Stale; before the final submit the row is retyped
// to Bea Substitute, a DIFFERENT real, unoriented roster person. The
// original code (perRow[i].ExistingID alone) would have authored Bea's
// orientation under a tick the officer made for Stan — this proves neither
// is authored, Bea is held out exactly as an untouched row would be, and
// the officer is told BY NAME that Stan's tick did not apply.
func TestStaleOrientationTickDoesNotAttributeToTheWrongPersonAndNotifies(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	stan := orientBlockPerson(t, w.st, "Stan Stale")
	bea := orientBlockPerson(t, w.st, "Bea Substitute")

	// The final submit: row 0 is the (already oriented) driver; row 1 now
	// names Bea, but the tick and its orient_target still name Stan — the
	// shape of a row edited after the warning was shown and ticked.
	body := scriptedCheckInBody("TRK-STALE1", 0, []passengerRow{knownRow(w.driver), knownRow(bea)})
	body.Set("add_orientation", "1")
	body.Set("orient_target", "1:"+stan.PersonID)
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code != 303 {
		t.Fatalf("POST /gate/checkin (stale tick, Bea a real known person) = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	if held, err := domain.OrientationHeld(w.st, stan.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Stan's orientation was recorded despite the row no longer naming him: held=%v err=%v", held, err)
	}
	if held, err := domain.OrientationHeld(w.st, bea.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Bea's orientation was recorded even though she was never legitimately ticked (the tick named Stan): held=%v err=%v", held, err)
	}
	open := orientBlockOpenPersonIDs(t, w.st)
	if !open[w.driver.PersonID] {
		t.Fatalf("the driver did not enter")
	}
	if open[bea.PersonID] {
		t.Fatalf("Bea entered despite lacking orientation and never being legitimately ticked")
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, staleOrientTickQueryKey+"="+stan.PersonID) {
		t.Fatalf("redirect %q does not carry Stan as a stale, unapplied tick", loc)
	}
	if strings.Contains(loc, staleOrientTickQueryKey+"="+bea.PersonID) {
		t.Fatalf("redirect %q wrongly carries Bea as a stale tick target — the tick never named her", loc)
	}
	// Bea IS genuinely held out (R-9, ordinary), so she also appears on the
	// NOT ENTERED list — the two notices name different facts about her.
	if !strings.Contains(loc, notEnteredQueryKey+"="+bea.PersonID) {
		t.Fatalf("redirect %q does not carry Bea as held out (she lacks the orientation and was never legitimately ticked)", loc)
	}

	page := orientBlockGet(t, w.srv, loc)
	notice := staleOrientTickNoticeText(page)
	if notice == "" {
		t.Fatalf("no stale-tick notice rendered after a tick went stale.\npage: %s", page)
	}
	for _, want := range []string{"Stan Stale", "Pad Site Control", "was NOT added and NOT recorded"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the stale-tick notice lacks %q.\nnotice: %s", want, notice)
		}
	}
	if strings.Contains(notice, "Bea Substitute") {
		t.Errorf("the stale-tick notice wrongly names Bea — the tick never named her.\nnotice: %s", notice)
	}
}

// TestStaleOrientationTickResolvingToNobodyAuthorsNothing is gap 2's
// original shape (the row resolves to NOBODY known, not a different known
// person): at an orientation=true post an off-roster row is refused
// outright (R-12), independent of this fix, so the whole commit 409s — this
// proves the stale tick's original target still gets NOTHING authored for
// it despite that refusal, i.e. completeTickedOrientations never has a
// side effect on a request the domain door goes on to refuse.
func TestStaleOrientationTickResolvingToNobodyAuthorsNothing(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	stan := orientBlockPerson(t, w.st, "Stan StaleToo")

	body := url.Values{
		"passenger_list": {"1"}, "direction": {gateInbound}, "vehicle": {"TRK-STALE2"},
		"driver_index": {"0"}, "time_source": {"default"},
		"occupant_index":  {"0", "1"},
		"occupant_typed":  {w.driver.Name, "Brand New Person"},
		"occupant_kind":   {"existing", "new"},
		"occupant_value":  {w.driver.PersonID, "Brand New Person"},
		"add_orientation": {"1"}, "orient_target": {"1:" + stan.PersonID},
	}
	rec := postForm(t, w.srv, "/gate/checkin", body)
	if rec.Code == 303 {
		t.Fatalf("POST /gate/checkin with an off-roster row at an orientation post = 303, want a refusal (R-12): %s", rec.Body.String())
	}
	if held, err := domain.OrientationHeld(w.st, stan.PersonID, "site-b"); err != nil || held != nil {
		t.Fatalf("Stan's orientation was recorded despite the row resolving to nobody and the commit being refused: held=%v err=%v", held, err)
	}
}

// TestGateHTMLEmitsOrientTargetAlongsideTheTick proves the wiring the
// server-side staleness check depends on: the accumulate render's warning
// carries orient_target = "<row index>:<person id>" beside the tick, for
// EVERY row showing the warning, ticked or not.
func TestGateHTMLEmitsOrientTargetAlongsideTheTick(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	body := scriptedCheckInBody("TRK-TARGET", 0, orientBlockRows(w.driver, w.uma))
	body.Set("new_last", ""); body.Set("new_first", "")
	rec := postForm(t, w.srv, "/gate/passenger/add", body)
	if rec.Code != 200 {
		t.Fatalf("POST /gate/passenger/add = %d: %s", rec.Code, rec.Body.String())
	}
	warning := crossSiteWarningText(rec.Body.String())
	want := `name="orient_target" value="1:` + w.uma.PersonID + `"`
	if !strings.Contains(warning, want) {
		t.Errorf("the warning lacks %q.\nwarning: %s", want, warning)
	}
}
