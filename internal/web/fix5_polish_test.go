package web

// fix5_polish_test.go — FIX5, the post-deploy polish batch (R7 L03). Each
// test FAILS on 35a0024 and passes after.

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestFix5UnsolicitedArrivalRefusesThisPostAsItsOwnOrigin — R7 L03-04
// (trips.go door vs trip_amend.go /arrivals/amend): the door accepted
// origin_site = this post's own id and RECORDED an arrival "from" itself,
// while /arrivals/amend refused that same value. One known list now: the
// peers. Both doors refuse it, in the same sentence, and record nothing.
func TestFix5UnsolicitedArrivalRefusesThisPostAsItsOwnOrigin(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	self := srv.Cfg.SiteID
	wm := w2Watermark(t, st, self, 1)
	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"}, "vehicle": {"ZZ-9"},
		"origin_site": {self}, "person_ids": {persons[0].PersonID},
	})
	body := html.UnescapeString(rec.Body.String())
	want := `"` + self + `" is not a post this post has an arrival from`
	if rec.Code != http.StatusConflict || !strings.Contains(body, want) {
		t.Fatalf("origin_site=%s (this post) = %d, want 409 %q; body:\n%s", self, rec.Code, want, body)
	}
	if got := w2Watermark(t, st, self, 1); got != wm {
		t.Fatalf("refused but authored %d event(s)", got-wm)
	}

	// /arrivals/amend refuses the same value with the same sentence.
	_, own := d87ForeignArrival(t, st, persons)
	wm = w2Watermark(t, st, self, 1)
	rec = postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {own}, "origin_site": {self}})
	body = html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, want) {
		t.Fatalf("/arrivals/amend origin_site=%s = %d, want 409 %q; body:\n%s", self, rec.Code, want, body)
	}
	if got := w2Watermark(t, st, self, 1); got != wm {
		t.Fatalf("/arrivals/amend refused but authored %d event(s)", got-wm)
	}
}

// TestFix5UnsolicitedOriginRefusalClosesLikeArrivalsAmend — R7 L03-04: the
// door said "… has an arrival from; nothing was recorded" (the domain
// error's lower-case tail), /arrivals/amend "… has an arrival from. Nothing
// was recorded." Same sentence, same close.
func TestFix5UnsolicitedOriginRefusalClosesLikeArrivalsAmend(t *testing.T) {
	srv, _, persons := newGateTestServer(t)
	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"}, "vehicle": {"ZZ-9"},
		"origin_site": {"banana"}, "person_ids": {persons[0].PersonID},
	})
	door := html.UnescapeString(rec.Body.String())
	const close = `"banana" is not a post this post has an arrival from. Nothing was recorded.`
	if rec.Code != http.StatusConflict || !strings.Contains(door, close) {
		t.Fatalf("door = %d, want 409 closing %q; body:\n%s", rec.Code, close, door)
	}
	if strings.Contains(door, "; nothing was recorded") {
		t.Fatalf("the door still closes with the lower-case domain tail:\n%s", door)
	}
	_, own := d87ForeignArrival(t, srv.Store, persons)
	rec = postForm(t, srv, "/arrivals/amend", url.Values{"arrival_id": {own}, "origin_site": {"banana"}})
	amend := html.UnescapeString(rec.Body.String())
	if !strings.Contains(amend, close) {
		t.Fatalf("CONTROL: /arrivals/amend no longer reads %q:\n%s", close, amend)
	}
}

// TestFix5ReturnedRefusalAtANonOriginPostNamesTheVehicle — R7 L03-01: a
// crafted POST /trips/returned at a post that did not send the run out
// read "trip ice-pad-trip-e1303baf4a631d5e was sent out by Ice Pad, …" —
// the raw trip id where every sibling line names the vehicle.
func TestFix5ReturnedRefusalAtANonOriginPostNamesTheVehicle(t *testing.T) {
	srv, st, _, persons := newOriginServer(t) // this post is site-b
	dest := "site-b"
	x := int64(30)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-ELSE-5"), DriverPersonID: persons[0].PersonID,
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	before := countEvents(t, st)
	rec := postForm(t, srv, "/trips/returned", url.Values{"time_source": {"default"}, "trip_id": {trip.TripID}})
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, "TRK-ELSE-5 was sent out by") {
		t.Fatalf("returned at a non-origin post = %d, want 409 naming TRK-ELSE-5; body:\n%s", rec.Code, body)
	}
	if strings.Contains(body, trip.TripID) {
		t.Fatalf("the refusal prints the raw trip id %s:\n%s", trip.TripID, body)
	}
	if countEvents(t, st) != before {
		t.Fatal("a refused return authored an event")
	}
}

// fix5OwnershipWords is the D-87 refusal as the officer reads it, pinned
// literally rather than through store.OwnershipRule, so the witness tests
// below cannot pass by tracking whatever the constant says.
const fix5OwnershipWords = "a site modifies only its own site's data"
