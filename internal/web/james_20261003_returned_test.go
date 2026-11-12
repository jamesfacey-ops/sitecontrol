package web

// James, 2026-10-03: a turned-back truck. The origin's OUTBOUND board carries
// an "Arrived back here" door (POST /trips/returned) that records the truck
// back at its origin — the trip closes as RETURNED, the crew is back on this
// post's muster, the red clears — authored as an ordinary arrival
// (domain.RecordReturnToOrigin; the two-post half is in
// deadline/james_20261003_return_test.go). Red before: no such door, the
// route 404/405'd and the board said "No arrival form here".

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

func TestTheOutboundBoardRecordsATurnedBackTruckBackAtItsOrigin(t *testing.T) {
	srv, st, d, persons := newOriginServer(t)
	trip, env := sendOut(t, st, persons[0], "TRK-BACK-1", 1)
	d.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(2 * time.Minute) }

	body := getAs(t, srv, "/trips")
	out := boardSection(t, body, "Outbound — on the road from")
	if !strings.Contains(out, `action="/trips/returned"`) || !strings.Contains(out, "Arrived back here") || !strings.Contains(out, `value="`+trip.TripID+`"`) {
		t.Fatalf("the outbound row offers no \"Arrived back here\" door:\n%s", out)
	}
	if !strings.Contains(topBarOf(t, body), "TRK-BACK-1") {
		t.Fatal("precondition: the overdue outbound truck must be in the bar")
	}

	rec := postForm(t, srv, "/trips/returned", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("returned = %d: %s", rec.Code, rec.Body.String())
	}
	body = getAs(t, srv, "/trips")
	if strings.Contains(boardSection(t, body, "Outbound — on the road from"), "TRK-BACK-1") {
		t.Fatal("the returned truck is still on the outbound board")
	}
	if strings.Contains(topBarOf(t, body), "TRK-BACK-1") {
		t.Fatal("the returned truck's red is still in the bar")
	}
	// Back ON SITE here — an open visit at the origin — not merely listed
	// as on the road (the origin's sheet lists its in-transit crew too).
	open, err := domain.OpenVisitsAt(st, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	var back bool
	for _, v := range open {
		back = back || v.PersonID == persons[0].PersonID
	}
	if !back {
		t.Fatalf("%s has no open visit at the origin after the return", persons[0].Name)
	}
	detail := getAs(t, srv, "/trips/detail")
	i := strings.Index(detail, trip.TripID)
	if i < 0 || !strings.Contains(detail[i:i+800], "<strong>RETURNED</strong> to") {
		t.Fatalf("the trips table does not mark the trip RETURNED:\n%s", detail[max(0, i):min(len(detail), i+800)])
	}

	// Already arrived (here): refused, nothing more authored.
	before := countEvents(t, st)
	rec = postForm(t, srv, "/trips/returned", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "recorded as arrived here") {
		t.Fatalf("second return = %d, want 409 recorded as arrived here: %s", rec.Code, rec.Body.String())
	}
	if countEvents(t, st) != before {
		t.Fatal("a refused return authored an event")
	}
}
