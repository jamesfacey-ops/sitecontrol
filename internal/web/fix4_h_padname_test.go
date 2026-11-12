package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

// TestFix4HUnconfiguredPadIsNamedReadably — L03-07 observation (fix4 H). A
// run to an uncontrolled pad replicates to posts that do not configure it;
// those posts printed the raw id ("conneco-pad (uncontrolled — no timer)",
// "AWAY AT A PAD — conneco-pad"), because SiteName's fallback returns a long
// id unchanged. FAIL BEFORE on every page below.
func TestFix4HUnconfiguredPadIsNamedReadably(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	srv.Cfg.Destinations = []config.Destination{{ID: "site-a", DisplayName: "Pad Site Control", Controlled: true}}
	srv.Cfg.LoginPassword = testLoginPassword
	pad := "conneco-pad"
	run, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "CON-1"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs:       []string{w.persons[1].PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	q := url.QueryEscape(run.TripID)
	for _, path := range []string{"/trips", "/trips/detail?trip_id=" + q, "/trips/amend?trip_id=" + q, "/muster"} {
		text := tags.ReplaceAllString(visibleText(w9bGet(t, srv, path)), " ")
		if strings.Contains(text, "conneco-pad") {
			i := strings.Index(text, "conneco-pad")
			t.Errorf("%s prints the raw pad id: …%s…", path, strings.Join(strings.Fields(text[max(0, i-80):min(len(text), i+40)]), " "))
		}
		if !strings.Contains(text, "Conneco Pad") {
			t.Errorf("%s does not name the pad \"Conneco Pad\"", path)
		}
	}
	for in, want := range map[string]string{"conneco-pad": "Conneco Pad", "north_pad": "North Pad", "": ""} {
		if got := padLabel(config.SiteLabel, in); got != want {
			t.Errorf("padLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := padLabel(func(string) string { return "Configured Name" }, "conneco-pad"); got != "Configured Name" {
		t.Errorf("a configured display name must win, got %q", got)
	}
}
