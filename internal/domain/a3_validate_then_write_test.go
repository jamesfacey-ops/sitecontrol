// a3_validate_then_write_test.go — ⟦A3 / cluster A, 2026-09-29⟧ the domain
// half of the refusal-that-still-writes fix: the two door changes that put a
// commit's extra writes BEHIND the door's own refusals rather than in front
// of them. DispatchInput.RecordChanges (H-5) and ResumeCheckInCompleting
// (H-6). The web tests (web/a3_refusal_writes_nothing_test.go) prove the
// same through the HTTP doors and the one-commit transaction; these pin the
// domain contract on a plain store, where nothing rolls back for free.
package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestA3DispatchAppliesRecordChangesOnlyAfterEveryRefusal: a departure that
// carries a confirmed company change and is refused (blank deadline toward a
// controlled destination) leaves the person's record untouched and the log
// unchanged; the same input with a deadline authors the amendment and then
// the trip, in that order, on consecutive seqs.
func TestA3DispatchAppliesRecordChangesOnlyAfterEveryRefusal(t *testing.T) {
	st := newDomainTestStore(t)
	before, err := CreateCompany(st, "site-a", 1, "Before Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	after, err := CreateCompany(st, "site-a", 1, "After Co", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := CreatePerson(st, "site-a", 1, "Dana Driver", &before.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "TRK-A3"
	veh, err := CreateVehicle(st, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest := "site-b"
	in := DispatchInput{
		VehicleID: veh.VehicleID, DriverPersonID: p.PersonID,
		DestinationSite: &dest, // controlled: a deadline is required
		RecordChanges:   []PersonRecordChange{{PersonID: p.PersonID, CompanyID: after.CompanyID}},
	}
	wm0, _ := st.Watermark("site-a", 1)
	if _, err := Dispatch(st, "site-a", 1, in, 0, "default"); err == nil || !strings.Contains(err.Error(), "requires a deadline") {
		t.Fatalf("precondition: the deadline-less departure was not refused for its deadline: %v", err)
	}
	if wm, _ := st.Watermark("site-a", 1); wm != wm0 {
		t.Fatalf("H-5 REPRODUCED at the domain: the refused departure authored %d event(s)", wm-wm0)
	}
	got, err := PersonByID(st, p.PersonID)
	if err != nil || got == nil || got.CompanyID == nil || *got.CompanyID != before.CompanyID {
		t.Fatalf("H-5 REPRODUCED at the domain: the refused departure changed the record: %+v (%v)", got, err)
	}

	dl := int64(60)
	in.DeadlineMinutes = &dl
	trip, err := Dispatch(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = PersonByID(st, p.PersonID)
	if got == nil || got.CompanyID == nil || *got.CompanyID != after.CompanyID {
		t.Fatalf("the valid departure did not apply the confirmed change: %+v", got)
	}
	evs, err := st.Events("site-a", 1, wm0+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range evs {
		var hdr struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(e.Payload, &hdr); err != nil {
			t.Fatal(err)
		}
		types = append(types, hdr.Type)
	}
	if len(types) != 2 || types[0] != evtPersonAmended || types[1] != "trip_dispatched" {
		// ONE amendment, THEN the trip — the order the web door always
		// authored them in, now kept inside the door.
		t.Fatalf("events after the valid departure = %v, want [%s trip_dispatched] (trip %s)", types, evtPersonAmended, trip.TripID)
	}
	if trip.Seq != evs[1].Seq {
		t.Fatalf("the trip's seq %d is not the last event's %d", trip.Seq, evs[1].Seq)
	}
}

// TestA3ResumeCheckInCompletingRefusesBeforeItTicks: on a trip that has
// already arrived, the resume is refused ("enters nobody") and the ticked
// orientation is NOT recorded; on a genuinely incomplete trip the same call
// records the tick and enters the person; a ticked id that is not aboard is
// ignored, never recorded.
func TestA3ResumeCheckInCompletingRefusesBeforeItTicks(t *testing.T) {
	st := newDomainTestStore(t)
	driver, err := CreatePerson(st, "site-b", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	uma, err := CreatePerson(st, "site-b", 1, "Uma Unoriented", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	bystander, err := CreatePerson(st, "site-b", 1, "Bo Bystander", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteOrientation(st, "site-b", 1, driver.PersonID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	num := "TRK-A3R"
	veh, err := CreateVehicle(st, "site-b", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, err := DispatchLocalCheckIn(st, "site-b", 1, DispatchInput{
		VehicleID: veh.VehicleID, DriverPersonID: driver.PersonID, OccupantPersonIDs: []string{uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// First resume, nobody ticked, orientation post: Uma held out, driver in.
	arr, err := ResumeCheckInCompleting(st, "site-b", 1, trip.TripID, nil, nil, 0, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(arr.NotEntered) != 1 || arr.NotEntered[0] != uma.PersonID {
		t.Fatalf("precondition: the first resume did not hold Uma out: %+v", arr.NotEntered)
	}
	wm0, _ := st.Watermark("site-b", 1)
	// The stale second resume, Uma ticked.
	_, err = ResumeCheckInCompleting(st, "site-b", 1, trip.TripID, []string{uma.PersonID}, nil, 0, "default", true)
	if err == nil || !strings.Contains(err.Error(), "enters nobody") {
		t.Fatalf("the second resume was not refused as already arrived: %v", err)
	}
	if held, _ := OrientationHeld(st, uma.PersonID, "site-b"); held != nil {
		t.Fatalf("H-6 REPRODUCED at the domain: the refused resume recorded Uma's orientation")
	}
	if wm, _ := st.Watermark("site-b", 1); wm != wm0 {
		t.Fatalf("H-6 REPRODUCED at the domain: the refused resume authored %d event(s)", wm-wm0)
	}

	// A fresh incomplete trip: the tick is honoured, a non-occupant's is not.
	trip2, err := DispatchLocalCheckIn(st, "site-b", 1, DispatchInput{
		VehicleID: veh.VehicleID, DriverPersonID: driver.PersonID, OccupantPersonIDs: []string{uma.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	arr, err = ResumeCheckInCompleting(st, "site-b", 1, trip2.TripID, []string{bystander.PersonID, uma.PersonID}, nil, 0, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(arr.NotEntered) != 0 {
		t.Fatalf("a ticked resume still held somebody out: %+v", arr.NotEntered)
	}
	if held, _ := OrientationHeld(st, uma.PersonID, "site-b"); held == nil {
		t.Fatalf("the ticked resume did not record Uma's orientation")
	}
	if held, _ := OrientationHeld(st, bystander.PersonID, "site-b"); held != nil {
		t.Fatalf("a ticked id that was NOT aboard was recorded — the trip-occupant scoping is gone")
	}
	open, err := OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	umaIn := false
	for _, v := range open {
		if v.PersonID == uma.PersonID {
			umaIn = true
		}
	}
	if !umaIn {
		t.Fatalf("the ticked resume did not enter Uma: %+v", open)
	}
}
