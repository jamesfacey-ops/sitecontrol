package web

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestFix4UnsolicitedArrivalRefusesAnOriginThatIsNotAPeer is L05 2b: a
// crafted POST /trips/arrive-unsolicited with origin_site=banana was
// RECORDED at every post, though /arrivals/amend refused the same value.
// Same rule, same M-4 wording; nothing recorded. The control (a peer) is
// still accepted.
func TestFix4UnsolicitedArrivalRefusesAnOriginThatIsNotAPeer(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	form := func(origin string) url.Values {
		return url.Values{
			"time_source": {"default"}, "vehicle": {"ZZ-9"},
			"origin_site": {origin}, "person_ids": {persons[0].PersonID},
		}
	}
	wm := w2Watermark(t, st, "site-a", 1)
	rec := postForm(t, srv, "/trips/arrive-unsolicited", form("banana"))
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusConflict || !strings.Contains(body, `"banana" is not a post this post has an arrival from`) {
		t.Fatalf("origin_site=banana = %d, want 409 in the M-4 wording; body:\n%s", rec.Code, body)
	}
	if got := w2Watermark(t, st, "site-a", 1); got != wm {
		t.Fatalf("refused but authored %d event(s)", got-wm)
	}
	if rec := postForm(t, srv, "/trips/arrive-unsolicited", form("site-b")); rec.Code != http.StatusSeeOther {
		t.Fatalf("control: origin_site=site-b (a peer) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
}
