// c1e_unref_none_surface_test.go — R3-RES-3 at the surface: the unref
// confirmation for a withdrawn "None of these" says what it was and that
// the arrival is parked again, never the pending copy; and the reopened
// rendering no longer claims the alert is live "at both posts".
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestC1eUnrefOfANoneResolutionConfirmsNothingReopensAndTheArrivalIsParkedAgain(t *testing.T) {
	srv, _, arrival, t1, t2 := c1cOwnParked(t)
	if rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "none": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("'none of these' = %d, want 303", rec.Code)
	}
	rec := postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {arrival}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /arrivals/unref on the none-resolution = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	conf := c1cGet(t, srv, rec.Header().Get("Location"))
	if strings.Contains(conf, "has not") && strings.Contains(conf, "reached this post yet") {
		t.Errorf("R3-RES-3: the confirmation renders the PENDING copy about this post's own arrival:\n%s", conf)
	}
	for _, want := range []string{"None of these", "nothing reopens", "back on the review lane", t1, t2, arrival} {
		if !strings.Contains(conf, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, conf)
		}
	}
	body := c1cGet(t, srv, "/trips")
	if !strings.Contains(body, `data-resolve-arrival="`+arrival+`"`) {
		t.Errorf("/trips does not offer the controls again after the unref")
	}
}

func TestC1eTheReopenedConfirmationDoesNotClaimTheAlertIsLiveAtBothPosts(t *testing.T) {
	srv, _, arrival, _, t2 := c1cOwnParked(t)
	if rec := postForm(t, srv, "/arrivals/resolve", url.Values{"arrival_id": {arrival}, "trip_id": {t2}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("resolution = %d, want 303", rec.Code)
	}
	rec := postForm(t, srv, "/arrivals/unref", url.Values{"arrival_id": {arrival}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unref = %d, want 303", rec.Code)
	}
	conf := c1cGet(t, srv, rec.Header().Get("Location"))
	if !strings.Contains(conf, "OPEN") || !strings.Contains(conf, "live again") {
		t.Fatalf("PRECONDITION: not the reopened rendering:\n%s", conf)
	}
	if strings.Contains(conf, "live again at both posts") {
		t.Errorf("the reopened copy still claims the alert is live at both posts — the outstanding-alerts list names only trips inbound to a post (R3-L13):\n%s", conf)
	}
}
