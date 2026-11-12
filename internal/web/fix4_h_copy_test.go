package web

import (
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// TestFix4HRemoveHelpIsTrueAtBothEnds — D-80 wording (fix4 H; L03-02
// observation). The refusal and the Remove help render at the origin AND at
// the destination; at the destination "here if it comes back" pointed at the
// wrong post, since a returning vehicle is checked in at its origin. FAIL
// BEFORE: both said "or here if it comes back".
func TestFix4HRemoveHelpIsTrueAtBothEnds(t *testing.T) {
	refusal := unsightedRemovalRefusal([]domain.OccupantRef{{PersonID: "p1", Name: "Ohara, Wendy"}})
	w := newRoadWorld(t)
	w.srv.Cfg.LoginPassword = testLoginPassword
	page := w9bGet(t, w.srv, "/trips/amend?trip_id="+url.QueryEscape(w.trip.TripID))
	page = strings.Join(strings.Fields(page), " ")
	for where, text := range map[string]string{"the D-80 refusal": refusal, "the /trips/amend Remove help": page} {
		if strings.Contains(text, "here if it comes back") {
			t.Errorf("%s still says a person leaves the list \"here if it comes back\" — untrue at the destination", where)
		}
		if !strings.Contains(text, "back at the post it left from if it comes back") {
			t.Errorf("%s does not name the post a returning vehicle is checked in at", where)
		}
	}
}

// TestFix4HHeadingsNameThePostOnce — R6-5 (the F3f family, fix4 H). The
// refusal page headed "Ice Pad (Ice Pad)": sitelabel is the display name
// once one is configured. The login and orientation headings had the same
// shape. FAIL BEFORE on all three.
func TestFix4HHeadingsNameThePostOnce(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	name := w.srv.Cfg.DisplayName
	cookie := loginCookie(t, w.srv, testLoginPassword)
	h1 := func(body string) string {
		m := regexp.MustCompile(`(?s)<h1>(.*?)</h1>`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		return strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], ""))
	}
	rreq := httptest.NewRequest("POST", "/trips/arrive", strings.NewReader(url.Values{"trip_id": {"site-z-trip-0000000000000000"}}.Encode()))
	rreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rreq.AddCookie(cookie)
	refusal := httptest.NewRecorder()
	w.srv.Root().ServeHTTP(refusal, rreq)
	if refusal.Code < 400 {
		t.Fatalf("precondition: a bogus /trips/arrive was not refused (%d)", refusal.Code)
	}
	req := httptest.NewRequest("GET", "/orientation", nil)
	req.AddCookie(cookie)
	orient := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(orient, req)
	for where, body := range map[string]string{
		"the refusal page": refusal.Body.String(),
		"/login":           orientBlockGet(t, w.srv, "/login"),
		"/orientation":     orient.Body.String(),
	} {
		got := h1(body)
		if n := strings.Count(got, name); n != 1 {
			t.Errorf("%s heading names the post %d times: %q", where, n, got)
		}
	}
}

// TestFix4HOfficerCopyCarriesNoFieldNamesOrDecisionIDs — L05 7d (fix4 H).
// /trips help printed the schema field "origin_site"; /arrivals/unref, the
// orientation refusal and a crafted dispatch refusal printed decision ids
// (D-87, D-18, D-77). Decision ids belong in code comments. FAIL BEFORE.
func TestFix4HOfficerCopyCarriesNoFieldNamesOrDecisionIDs(t *testing.T) {
	w := newOrientBlockWorld(t, true)
	decision := regexp.MustCompile(`\bD-\d+\b`)
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	cookie := loginCookie(t, w.srv, testLoginPassword)
	for _, path := range []string{"/trips", "/arrivals/unref"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		w.srv.Root().ServeHTTP(rec, req)
		text := tags.ReplaceAllString(visibleText(rec.Body.String()), " ")
		if strings.Contains(text, "origin_site") {
			t.Errorf("%s prints the field name origin_site", path)
		}
		if hit := decision.FindString(text); hit != "" {
			t.Errorf("%s prints decision id %s", path, hit)
		}
	}
	for what, msg := range map[string]string{
		"orientation refusal": w.srv.orientationRefusalCopy(&domain.OrientationRefusal{Code: domain.OrientationPersonUnknown, PersonName: "Ohara, Wendy"}),
		"dispatch refusal": func() string {
			var in domain.DispatchInput
			_, err := applyDispatchDestination(w.srv.Cfg, "", "site-z", "60", &in)
			if err == nil {
				t.Fatal("precondition: an unknown destination site was not refused")
			}
			return err.Error()
		}(),
	} {
		if hit := decision.FindString(msg); hit != "" {
			t.Errorf("the %s prints decision id %s: %q", what, hit, msg)
		}
	}
}
