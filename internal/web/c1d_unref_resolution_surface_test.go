// c1d_unref_resolution_surface_test.go — James, 2026-09-30: unref undoes a
// resolution, at the surface: POST /arrivals/unref on a resolved arrival
// lands, the confirmation says the resolution was withdrawn and the
// arrival is parked again, /trips offers the controls again, and a
// re-resolution lands.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func TestC1dUnrefOfAResolvedArrivalReopensTheTripAndOffersTheControlsAgain(t *testing.T) {
	srv, st, arrival, t1, t2 := c1cOwnParked(t)
	if rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "trip_id": {t2}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("resolution = %d, want 303", rec.Code)
	}
	if ts, _ := domain.TripStateByID(st, t2); ts == nil || ts.State != domain.TripStateArrived {
		t.Fatalf("PRECONDITION: trip2 %+v, want arrived", ts)
	}
	before := w2Watermark(t, st, "site-a", 1)

	rec := postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {arrival}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /arrivals/unref on the resolved arrival = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	if got := w2Watermark(t, st, "site-a", 1); got != before+1 {
		t.Fatalf("appended %d event(s), want exactly 1", got-before)
	}
	conf := c1cGet(t, srv, rec.Header().Get("Location"))
	for _, want := range []string{t2, "OPEN", "withdrew a", "resolution", "back on the review lane", arrival, t1} {
		if !strings.Contains(conf, want) {
			t.Errorf("unref confirmation lacks %q:\n%s", want, conf)
		}
	}
	if ts, _ := domain.TripStateByID(st, t2); ts == nil || ts.State != domain.TripStateOpen {
		t.Fatalf("trip2 %+v, want open again", ts)
	}
	body := c1cGet(t, srv, "/trips")
	for _, want := range []string{`data-resolve-arrival="` + arrival + `"`, `name="trip_id" value="` + t1 + `"`, `name="trip_id" value="` + t2 + `"`, "None of these (plain visit)"} {
		if !strings.Contains(body, want) {
			t.Errorf("/trips lacks %q after the unref", want)
		}
	}
	// Re-resolve, correctly this time.
	if rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "trip_id": {t1}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("re-resolution = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	if ts, _ := domain.TripStateByID(st, t1); ts == nil || ts.State != domain.TripStateArrived || ts.ArrivalID == nil || *ts.ArrivalID != arrival {
		t.Fatalf("trip1 %+v, want arrived by %s", ts, arrival)
	}
	if ts, _ := domain.TripStateByID(st, t2); ts == nil || ts.State != domain.TripStateOpen {
		t.Fatalf("trip2 %+v, want still open", ts)
	}
}

func TestC1dUnrefOfAParkedButUnresolvedArrivalIsStillRefused(t *testing.T) {
	srv, st, arrival, _, _ := c1cOwnParked(t)
	before := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {arrival}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no standing resolution") {
		t.Fatalf("POST /arrivals/unref on an unresolved parked arrival = %d, want 409 naming no standing resolution; body:\n%s", rec.Code, rec.Body.String())
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Errorf("authored %d event(s) while refusing", after-before)
	}
}
