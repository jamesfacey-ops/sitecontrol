// r3_unack_capability_test.go — ruling R3, first half (James, 2026-10-04):
// "a post that cannot ack an alert should not be able to un-ack (re-arm)
// it." L08 T-100 group: npf, which has no board row for an ice-road trip and
// is refused "not on the deadline board" at the ack door, could still POST
// /alerts/unack and re-arm the red at both ice posts.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

func TestR3APostThatCannotAckATripCannotUnackIt(t *testing.T) {
	srv, st, d, mine := newAlertingServer(t)

	// A trip between two OTHER posts (site-a -> site-c), overdue, acked at
	// its destination — replicated here like everything else.
	dest := "site-c"
	x := int64(2)
	theirs, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "ICE TRUCK 9"), DriverPersonID: "driver-a",
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := st.Events("site-a", 1, theirs.Seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("fetch trip envelope: %v", err)
	}
	d.OnApplied(envs[0], time.Now().Add(-(2*time.Minute + 30*time.Second)), true)
	theirPost := "SITE C GATE"
	if err := deadline.Acknowledge(st, "site-c", 1, theirs.TripID, &theirPost, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Precondition: the ack door refuses this post for that trip.
	if rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {theirs.TripID}}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not on the deadline board") {
		t.Fatalf("precondition: the ack door must refuse a trip not on this post's board, got %d %s", rec.Code, rec.Body.String())
	}

	before := countEvents(t, st)
	rec := postForm(t, srv, "/alerts/unack", url.Values{"trip_id": {theirs.TripID}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "cannot withdraw an acknowledgment") {
		t.Fatalf("R3: un-ack of a trip this post cannot ack = %d %q, want 409 refusal", rec.Code, rec.Body.String())
	}
	if countEvents(t, st) != before {
		t.Fatal("R3: the refused un-ack authored an event")
	}
	if acks, err := deadline.EffectiveAcks(st, theirs.TripID); err != nil || len(acks) != 1 {
		t.Fatalf("R3: site-c's ack must still stand, got %v %v", acks, err)
	}
	// ⟦was: the phase-scoped un-ack (review_phase=3) refused here too with
	// 409 — weather phases removed, James 2026-10-04; a review_phase is now
	// refused at every post (400, fix3_no_weather_test.go)⟧

	// Positive control: this post's own trip — ack then un-ack, both taken.
	if rec := postForm(t, srv, "/alerts/ack", url.Values{"trip_id": {mine.TripID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("own ack = %d %s", rec.Code, rec.Body.String())
	}
	if rec := postForm(t, srv, "/alerts/unack", url.Values{"trip_id": {mine.TripID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("own un-ack = %d %s", rec.Code, rec.Body.String())
	}
}
