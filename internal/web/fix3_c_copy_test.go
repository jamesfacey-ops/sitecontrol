package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
)

// FIX3 stream C — the raw-site-id and copy sweep. James, 2026-10-04: no
// screen shows a raw site id. Each test below FAILS on e3d507d.

// F3c: the foreign road-work row's label names the origin by display name.
func TestFix3C_ForeignRoadWorkRowNamesOriginByDisplayName(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	limit := int64(60)
	if _, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "GRADER 3"), DriverPersonID: w.crew[1].PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, time.Now().UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
	rows, err := w.srv.roadWorkRows(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Own {
		t.Fatalf("want one foreign row, got %+v", rows)
	}
	if got, want := rows[0].UrgencyLabel, "CLOCK AT Pad Site Control"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if rows[0].OriginName != "Pad Site Control" {
		t.Errorf("OriginName = %q, want the display name", rows[0].OriginName)
	}
}

// F3e: the intake duplicate warning says where somebody is enrolled by name.
func TestFix3C_IntakeDuplicateWarningNamesPostNotID(t *testing.T) {
	m := personMatch{Person: domain.Person{Name: "Ashton, Joss", SiteID: "ice-road"}}
	got := intakeDuplicateRefusal([]personMatch{m}, nil, func(id string) string { return "Ice Road Entrance" })
	if strings.Contains(got, "ice-road") || !strings.Contains(got, "enrolled at Ice Road Entrance") {
		t.Errorf("duplicate warning: %q", got)
	}
}

// F3f: "Ice Pad (Ice Pad) status page" — the name once.
func TestFix3C_StatusHeadingNamesThePostOnce(t *testing.T) {
	srv := newTestServer(t)
	body := getStatusPage(t, srv)
	m := regexp.MustCompile(`(?s)<h1>(.*?)</h1>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no h1 on /status")
	}
	h1 := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "")
	if n := strings.Count(h1, srv.Cfg.DisplayName); n != 1 {
		t.Errorf("/status heading names the post %d times: %q", n, strings.TrimSpace(h1))
	}
}

// The intake note no longer names a config key.
func TestFix3C_IntakeNoEntranceNoteNamesNoConfigKey(t *testing.T) {
	b, err := templateFS.ReadFile("templates/intake.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<code>entrance_site_id</code>") {
		t.Error("intake.html still shows the config key entrance_site_id to the officer")
	}
}

// F3h: the ledger cell. The domain keeps its canonical rendering for the
// oracle; only the page translates it.
func TestFix3C_DestinationLedgerCellUsesDisplayName(t *testing.T) {
	name := func(id string) string {
		if id == "ice-pad" {
			return "Ice Pad"
		}
		return id
	}
	for in, want := range map[string]string{
		"controlled ice-pad, 90 min": "controlled Ice Pad, 90 min",
		"uncontrolled ice-pad":       "uncontrolled Ice Pad",
		"aboard":                     "aboard",
		"cleared":                    "cleared",
	} {
		if got := destinationAssertedProse(name, in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// T-097: "Back button … what you typed" only where a form was typed into.
func TestFix3C_RefusalBackSentenceOnlyWhereFormStateExists(t *testing.T) {
	mk := func(method, path, referer string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		if referer != "" {
			r.Header.Set("Referer", "http://x.example"+referer)
		}
		return r
	}
	for _, c := range []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"typed form", mk("POST", "/trips/arrive-unsolicited", "/trips"), true},
		{"one-button ack", mk("POST", "/alerts/ack", "/trips"), false},
		{"one-button unack", mk("POST", "/alerts/unack", "/trips"), false},
		{"crafted, no page behind it", mk("POST", "/trips/arrive", ""), false},
		{"a GET", mk("GET", "/trips", "/trips"), false},
	} {
		if got := refusalHasFormState(c.r); got != c.want {
			t.Errorf("%s: refusalHasFormState = %v, want %v", c.name, got, c.want)
		}
	}
}

// Crawl: with display names configured, no page shows a site id as a whole
// word in prose. Tag markup (attributes, URLs, data-*) and input values are
// not prose, so every tag is stripped first. The /status configuration table
// lists TOML keys against live values by design (its site_id row), so that
// one row is exempt.
func TestFix3C_NoRenderedPageShowsARawSiteIDInProse(t *testing.T) {
	w := newRoadWorkWeb(t, true)
	limit := int64(60)
	// A foreign road-work run and a normal outbound run, so the rows that
	// name another post exist on the pages.
	if _, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "GRADER 3"), DriverPersonID: w.crew[1].PersonID,
		RoadWork: true, DeadlineMinutes: &limit,
	}, time.Now().UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`\bsite-[ab]\b`)
	derived := regexp.MustCompile(`site-[ab]-(trip|person|vehicle)-[0-9a-f]+`) // minted ids, not site names
	cfgRow := regexp.MustCompile(`(?s)<tr><th>site_id</th>.*?</tr>`)
	tags := regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]*>`)
	for _, route := range []string{"/status", "/trips", "/trips/detail", "/gate", "/gate/vehicle", "/muster", "/analytics", "/roster", "/analytics/site/site-b"} {
		body := w.get(t, route, true)
		text := tags.ReplaceAllString(derived.ReplaceAllString(cfgRow.ReplaceAllString(body, ""), ""), " ")
		if hit := ids.FindString(text); hit != "" {
			i := strings.Index(text, hit)
			lo, hi := max(0, i-60), min(len(text), i+60)
			t.Errorf("%s shows raw site id %q in prose: …%s…", route, hit, strings.Join(strings.Fields(text[lo:hi]), " "))
		}
	}
}
