// d78_close_cancel_test.go — [[D-78]] (2026-09-05): the cancel route is
// CLOSED, and the refusal that used to recommend it says WAIT UNTIL IT
// ARRIVES. Follows [[D-73]] (James, verbatim): "no trip will be cancled they
// will continue on to the next post then leave that post with a second trip".
//
// D-78 removed the ONE officer-facing door to domain.CancelTrip — the
// POST /trips/cancel route and the Cancel button on /trips/detail — and KEPT
// the `cancelled` / `arrived_after_cancel` states, because already-replicated
// history carrying them must still fold. The domain function stays; nothing
// an officer can reach calls it.
//
// WHAT THIS FILE PROVES, and why every negative is CONTROLLED:
//
//  1. NO OFFICER-FACING ROUTE REACHES CancelTrip — structurally (the mux has
//     no handler for POST /trips/cancel; the W2 route-walk's own universe
//     scan finds no registration literal) AND behaviourally (the old POST,
//     with a real login, driven in all three time-control shapes, authors
//     nothing and the trip stays open). The fixture is the ORIGIN post's own
//     open trip — the one shape the domain would have cancelled — and the
//     control calls domain.CancelTrip directly on it afterwards and watches
//     the state move. The store CAN show a cancel; the route cannot produce
//     one.
//
//  2. THE ITEM-5 SCENARIO CANNOT RECUR: three people on the printed muster's
//     ON THE ROAD section, the old POST attempted, all three still named and
//     still counted. Controlled the same way: the direct domain cancel DOES
//     empty the section (amendment 192, re-measured), so the sheet can tell
//     the two apart.
//
//  3. THE THIRD COPY. tripauthor.go's all-stayed-behind refusal reaches the
//     officer verbatim through refuse(); it no longer says "(or cancel the
//     trip)". Asserted on the served body, with the arm that DOES author as
//     the control.
//
// ⚠ NOTHING HERE IS ASSERTED ON A STATUS CODE ALONE. This house's catch-all
// answers 200 to a great deal; the store and the rendered sheet are the
// witnesses.
package web

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// d78OldCancelPost is the exact body the removed Cancel button used to
// submit (trips_detail.html:38-41 at 42f45b8): the trip id plus the time
// control's untouched default.
func d78OldCancelPost(tripID string) url.Values {
	return url.Values{"trip_id": {tripID}, w2TimeSourceField: {"default"}}
}

// d78RequireOpen fails unless the trip's folded state reads `open`.
func d78RequireOpen(t *testing.T, st *store.Store, tripID, when string) {
	t.Helper()
	state, err := domain.TripStateByID(st, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || state.State != domain.TripStateOpen {
		t.Fatalf("D-78: %s, trip %s reads %v, want %s — the state the route used to produce must be unreachable from it",
			when, tripID, state, domain.TripStateOpen)
	}
}

// TestD78NoOfficerFacingRouteReachesCancelTrip — the headline witness.
func TestD78NoOfficerFacingRouteReachesCancelTrip(t *testing.T) {
	// --- structural: the door is not there ------------------------------
	srv, st := w9aOriginServer(t, "d78-route.db") // site-a, the ORIGIN
	if w9aRouteExists(t, srv, http.MethodPost, "/trips/cancel") {
		t.Fatalf("D-78: the mux still has a handler for POST /trips/cancel — the route is to be CLOSED, not merely unlinked from the pages")
	}
	for _, f := range w2PostRegistrationsInDir(t, filepath.Join(w2ModuleRoot(t), w2WebDir)) {
		if f.Route == "/trips/cancel" {
			t.Fatalf("D-78: a \"POST /trips/cancel\" registration literal survives at %s:%d", f.File, f.Line)
		}
	}

	// --- behavioural: the old POST authors nothing, in every time shape --
	w9aSeedPerson(t, st, "site-r", 1, "p-d78-drv", "Dana Driver")
	w9aSeedPerson(t, st, "site-r", 1, "p-d78-pax", "Pat Passenger")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-D78", "p-d78-drv", []string{"p-d78-pax"}, 240)
	d78RequireOpen(t, st, trip.TripID, "before any POST")

	shapes := []struct {
		name  string
		shape func(url.Values)
	}{
		{"untouched-default", func(url.Values) {}},
		{"entered-override", func(f url.Values) {
			f.Set(w2TimeSourceField, "entered")
			f.Set(w2OccurredAtField, w2Wall(w2Base-30*w2Minute))
		}},
		{"signal-less", func(f url.Values) { f.Del(w2TimeSourceField); f.Del(w2OccurredAtField) }},
	}
	for _, sh := range shapes {
		form := d78OldCancelPost(trip.TripID)
		sh.shape(form)
		before := w2Watermark(t, st, "site-a", 1)
		rec, _ := w2PostForm(t, srv, "/trips/cancel", form)
		if after := w2Watermark(t, st, "site-a", 1); after != before {
			var types []string
			for _, e := range w2EventsFrom(t, st, "site-a", 1, before) {
				types = append(types, w2PayloadType(t, e))
			}
			t.Fatalf("D-78 [%s]: POST /trips/cancel AUTHORED %d event(s) (watermark %d -> %d; types %v). The route is closed by ruling — "+
				"nothing an officer can reach may write trip_cancelled. Response %d:\n%s", sh.name, after-before, before, after, types, rec.Code, rec.Body.String())
		}
		d78RequireOpen(t, st, trip.TripID, "after the "+sh.name+" POST")
	}

	// --- CONTROL: the domain WOULD have cancelled this very trip ---------
	// Without this, the negatives above prove nothing about the ROUTE: a
	// non-origin fixture, or an already-closed trip, is refused by the domain
	// whether or not any route reaches it.
	if err := domain.CancelTrip(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("D-78 CONTROL FAILED: domain.CancelTrip refused the fixture trip (%v) — the fixture is not the shape the route used to cancel", err)
	}
	state, err := domain.TripStateByID(st, trip.TripID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || state.State != domain.TripStateCancelled {
		t.Fatalf("D-78 CONTROL FAILED: after a direct domain cancel the trip reads %v, want %s — the store cannot show a cancel, so its silence after the POST is not evidence",
			state, domain.TripStateCancelled)
	}
}

// TestD78ThreeOnTheRoadSurviveTheOldCancelPost — decision-sheet item 5,
// closed at source. Amendment 192: three people on the printed muster, one
// click on Cancel, zero of them on any sheet at either post. The click has
// nowhere to land now.
func TestD78ThreeOnTheRoadSurviveTheOldCancelPost(t *testing.T) {
	w := newRoadWorld(t)
	// The muster fixture is built without a login; the old POST has to get
	// past requireLogin to prove anything about the route behind it.
	w.srv.Cfg.LoginPassword = testLoginPassword

	page := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster/print"), "")
	if !hasRoadSection(page) {
		t.Fatalf("precondition: the crew is not on the road before the POST:\n%s", page)
	}
	sec := roadSection(t, page)
	for _, p := range w.riding {
		if !strings.Contains(sec, p.Name) {
			t.Fatalf("precondition: %s is not named on the road section before the POST:\n%s", p.Name, sec)
		}
	}
	if !strings.Contains(page, `data-tally-count="in-transit">3<`) {
		t.Fatalf("precondition: the road headcount is not 3:\n%s", page)
	}

	before := w2Watermark(t, w.st, "site-a", 1)
	rec := postForm(t, w.srv, "/trips/cancel", d78OldCancelPost(w.trip.TripID))

	after := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster/print"), "")
	if !hasRoadSection(after) {
		t.Fatalf("D-78 / item 5 RECURRED: one POST to the old cancel route and the printed muster has NO road section — %d people gone from every sheet at this post "+
			"while the record still says they are aboard. Response %d:\n%s\nSheet:\n%s", len(w.riding), rec.Code, rec.Body.String(), after)
	}
	sec = roadSection(t, after)
	for _, p := range w.riding {
		if !strings.Contains(sec, p.Name) {
			t.Errorf("D-78 / item 5: %s is no longer named on the road section after the old cancel POST:\n%s", p.Name, sec)
		}
	}
	if !strings.Contains(after, `data-tally-count="in-transit">3<`) {
		t.Errorf("D-78 / item 5: the road headcount is no longer 3 after the old cancel POST:\n%s", after)
	}
	if wm := w2Watermark(t, w.st, "site-a", 1); wm != before {
		t.Errorf("D-78: the old cancel POST authored %d event(s) (response %d: %s)", wm-before, rec.Code, rec.Body.String())
	}
	d78RequireOpen(t, w.st, w.trip.TripID, "after the old cancel POST")
	if t.Failed() {
		t.FailNow()
	}

	// --- CONTROL: the retained STATE still empties the sheet ------------
	// The domain cancel is authored directly, as every test pinning the
	// retained state does. If this did NOT empty the section, the sheet could
	// not have shown the item-5 loss and the negatives above would be
	// uncontrolled.
	if err := domain.CancelTrip(w.st, "site-a", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("D-78 CONTROL: the direct domain cancel was refused: %v", err)
	}
	if hasRoadSection(rawPage(t, w.srv, "/muster/print")) {
		t.Fatalf("D-78 CONTROL FAILED: a direct domain cancel left the road section in place — the sheet cannot tell a cancelled run from an open one, so its stability after the POST is not evidence")
	}
}

// TestD78AllStayedBehindRefusalNamesTheWaitPathNotACancel — the third
// cancel instruction, the one only the surface map found: a DOMAIN error
// string that reaches the officer verbatim through refuse() when every
// occupant is ticked "stayed behind" on the arrival form.
func TestD78AllStayedBehindRefusalNamesTheWaitPathNotACancel(t *testing.T) {
	srv, st, persons := newTripsTestServer(t) // site-b, the destination
	tripID := injectForeignTrip(t, st, 1, "BUS 78", 60, persons[0].PersonID, persons[1].PersonID)

	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec := postForm(t, srv, "/trips/arrive", url.Values{
		w2TimeSourceField: {"default"}, "trip_id": {tripID},
		"stayed_person_ids": {persons[0].PersonID, persons[1].PersonID}, // everybody
	})
	body := rec.Body.String()
	if after := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch); after != before {
		t.Fatalf("an arrival with NOBODY aboard authored %d event(s) — the refusal that carries this copy did not refuse. Body:\n%s", after-before, body)
	}
	if strings.Contains(body, "or cancel the trip") {
		t.Fatalf("D-78: the all-stayed-behind refusal still tells the officer to \"cancel the trip\" — the third of the three cancel instructions "+
			"the surface map found (tripauthor.go:645), reaching the officer verbatim through refuse(). Body:\n%s", body)
	}
	for _, want := range []string{"record nothing", "a trip is never cancelled", "untick"} {
		if !strings.Contains(body, want) {
			t.Fatalf("D-78: the all-stayed-behind refusal does not say %q — the officer who ticked everyone must be told what TO do: untick whoever "+
				"is aboard, or record nothing and leave the trip open. Body:\n%s", want, body)
		}
	}

	// --- CONTROL: one person actually aboard, and the same POST authors --
	rec = postForm(t, srv, "/trips/arrive", url.Values{
		w2TimeSourceField: {"default"}, "trip_id": {tripID},
		"stayed_person_ids": {persons[1].PersonID},
	})
	open, err := domain.OpenVisitsAt(st, srv.Cfg.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].PersonID != persons[0].PersonID {
		t.Fatalf("CONTROL FAILED: an arrival with ONE person aboard did not author their visit (%d: %s) — open visits: %+v", rec.Code, rec.Body.String(), open)
	}
}
