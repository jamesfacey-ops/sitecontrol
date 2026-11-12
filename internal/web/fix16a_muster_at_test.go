package web

// fix16a_muster_at_test.go — FIX16a (James, 2026-10-06): the muster for any
// moment (/muster/at) and a person's last sighting (/last-seen). Every test
// here fails on 1cbf34f (neither route exists: the GET falls through to the
// status page) and passes after.

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fix16aWorld is two posts' records held at site-b ("Ice Camp B"); site-a
// ("Ice Pad") replicated in. All on 2026-09-29; the moment asked about is
// 10:00 unless a test says otherwise.
//
//	site-a 08:30  Joss checks in at Ice Pad (stays)
//	site-b 08:00  Ann checks in (stays)                          → on site
//	site-b 08:00  Ben checks in; signed out at 09:00, typed by hand
//	              at 11:00                                        → left before
//	site-b 11:00  Cal checks in; corrected to 09:30               → on site (corrected)
//	site-b 09:00  Dee checks in; corrected to 10:30               → not yet there
//	site-b 12:00  Eve's check-in typed by hand as 09:15           → on site (hand)
//	site-b 08:00  Hal checks in; 09:00 TRK-20 to Ice Pad;
//	              09:45 turned back to Ice Camp B                 → on site (turn-back)
//	site-b 08:00  Fay, Gus, Ivy check in; 09:30 TRK-12 to Ice Pad,
//	              Fay driving; Ivy taken off by a correction       → Fay, Gus on the road;
//	                                                                Ivy not tracked
//	site-a 11:00  TRK-12 arrives at Ice Pad
//	site-b 10:30  Kim checks in on an UNSYNCED clock               → could not be placed
type fix16aWorld struct {
	srv                                         *Server
	ann, ben, cal, dee, eve, fay, gus, hal, ivy domain.Person
	joss, kim                                   domain.Person
	trk12, trk20                                string
}

// fix16aCheckIn is a gate check-in returning its arrival (for a correction).
func fix16aCheckIn(t *testing.T, st *store.Store, site, truck string, at int64, source string, people ...string) domain.Arrival {
	t.Helper()
	trip, err := domain.DispatchLocalCheckIn(st, site, 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, site, truck), DriverPersonID: people[0], OccupantPersonIDs: people[1:],
	}, at, source)
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.RecordExpectedArrival(st, site, 1, trip.TripID, nil, at, source)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func fix16aOpenVisit(t *testing.T, st *store.Store, site, person string) string {
	t.Helper()
	open, err := domain.OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range open {
		if v.PersonID == person {
			return v.VisitID
		}
	}
	t.Fatalf("no open visit for %s at %s", person, site)
	return ""
}

func newFix16aWorld(t *testing.T) fix16aWorld {
	t.Helper()
	a, setA := anStore(t)
	b, setB := anStore(t)
	var w fix16aWorld
	dest, mins := "site-a", int64(90)

	setA(anLocal(7, 0))
	w.joss = fix9Person(t, a, "site-a", "Ashton", "Joss", "Kakivik", "(907) 555-4471")
	setA(anLocal(8, 30))
	fix16aCheckIn(t, a, "site-a", "TRK-7", 0, "default", w.joss.PersonID)
	fgReplicate(t, b, a, "site-a")

	setB(anLocal(7, 0))
	w.ann = fix9Person(t, b, "site-b", "Able", "Ann", "Alpha Drilling", "(907) 555-0001")
	w.ben = fix9Person(t, b, "site-b", "Baker", "Ben", "Alpha Drilling", "")
	w.cal = fix9Person(t, b, "site-b", "Cole", "Cal", "Beta Haul", "")
	w.dee = fix9Person(t, b, "site-b", "Dunn", "Dee", "Beta Haul", "")
	w.eve = fix9Person(t, b, "site-b", "Evans", "Eve", "Beta Haul", "")
	w.fay = fix9Person(t, b, "site-b", "Fox", "Fay", "Polar Haul", "")
	w.gus = fix9Person(t, b, "site-b", "Gray", "Gus", "Polar Haul", "(907) 555-0007")
	w.hal = fix9Person(t, b, "site-b", "Hill", "Hal", "Polar Haul", "")
	w.ivy = fix9Person(t, b, "site-b", "Ives", "Ivy", "Polar Haul", "")
	w.kim = fix9Person(t, b, "site-b", "Kerr", "Kim", "Polar Haul", "")

	setB(anLocal(8, 0))
	fix16aCheckIn(t, b, "site-b", "TRUCK 1", 0, "default", w.ann.PersonID)
	fix16aCheckIn(t, b, "site-b", "TRUCK 2", 0, "default", w.ben.PersonID)
	fix16aCheckIn(t, b, "site-b", "TRUCK 3", 0, "default", w.hal.PersonID)
	fix16aCheckIn(t, b, "site-b", "TRUCK 4", 0, "default", w.fay.PersonID, w.gus.PersonID, w.ivy.PersonID)

	setB(anLocal(9, 0))
	dee := fix16aCheckIn(t, b, "site-b", "TRUCK 5", 0, "default", w.dee.PersonID)
	trk20, err := domain.Dispatch(b, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, b, "site-b", "TRK-20"), DriverPersonID: w.hal.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.trk20 = trk20.TripID

	setB(anLocal(9, 30))
	trk12, err := domain.Dispatch(b, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, b, "site-b", "TRK-12"), DriverPersonID: w.fay.PersonID,
		OccupantPersonIDs: []string{w.gus.PersonID, w.ivy.PersonID}, DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.trk12 = trk12.TripID

	setB(anLocal(9, 45))
	if _, err := domain.RecordReturnToOrigin(b, "site-b", 1, w.trk20, nil, nil, 0, "default", false); err != nil {
		t.Fatal(err)
	}

	setB(anLocal(10, 30))
	b.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: anLocal(10, 30), MaxErrorUS: 500, State: store.ClockUnsynced}
	}
	fix16aCheckIn(t, b, "site-b", "TRUCK 9", 0, "default", w.kim.PersonID)
	var now int64
	b.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: now, MaxErrorUS: 500, State: store.ClockSynced}
	}
	setB = func(ms int64) { now = ms }

	setB(anLocal(11, 0))
	cal := fix16aCheckIn(t, b, "site-b", "TRUCK 6", 0, "default", w.cal.PersonID)
	by := "Officer Kay"
	if err := domain.CheckOutVisit(b, "site-b", 1, fix16aOpenVisit(t, b, "site-b", w.ben.PersonID), &by, anLocal(9, 0), "entered"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []domain.TimeCorrection{
		{Target: store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: cal.Seq}, CorrectedOccurredAtMS: anLocal(9, 30), RecordedBy: by},
		{Target: store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: dee.Seq}, CorrectedOccurredAtMS: anLocal(10, 30), RecordedBy: by},
	} {
		if _, err := domain.CorrectOccurredAt(b, "site-b", 1, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := domain.AmendTrip(b, "site-b", 1, domain.TripAmendment{TripID: w.trk12, OccupantsRemove: []string{w.ivy.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	setB(anLocal(12, 0))
	fix16aCheckIn(t, b, "site-b", "TRUCK 8", anLocal(9, 15), "entered", w.eve.PersonID)

	fgReplicate(t, a, b, "site-b")
	setA(anLocal(11, 0))
	if _, err := domain.RecordExpectedArrival(a, "site-a", 1, w.trk12, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	fgReplicate(t, b, a, "site-a")
	setB(anLocal(13, 0))

	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: b.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Pad", Controlled: true, TravelMinutes: 65}},
		LoginPassword: testLoginPassword,
	}
	w.srv = New(cfg, b)
	return w
}

func fix16aGet(t *testing.T, srv *Server, host, path string, q url.Values, login bool) (int, string) {
	t.Helper()
	u := path
	if q != nil {
		u += "?" + q.Encode()
	}
	req := httptest.NewRequest("GET", u, nil)
	if host != "" {
		req.Host = host
	}
	if login {
		req.AddCookie(loginCookie(t, srv, testLoginPassword))
	}
	rec := httptest.NewRecorder()
	srv.Root().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func fix16aMoment(t *testing.T, w fix16aWorld, site, at string) string {
	t.Helper()
	code, body := fix16aGet(t, w.srv, "", "/muster/at", url.Values{"site": {site}, "date": {"2026-09-29"}, "time": {at}}, true)
	if code != http.StatusOK {
		t.Fatalf("GET /muster/at %s %s = %d", site, at, code)
	}
	return body
}

// fix16aSection returns one section of one site's sheet.
func fix16aSection(t *testing.T, body, site, section string) string {
	t.Helper()
	i := strings.Index(body, `data-muster-at-site="`+site+`"`)
	if i < 0 {
		t.Fatalf("no sheet for %s:\n%s", site, body)
	}
	sheet := body[i:]
	if j := strings.Index(sheet[1:], `data-muster-at-site="`); j >= 0 {
		sheet = sheet[:j+1]
	}
	k := strings.Index(sheet, `data-muster-at-section="`+section+`"`)
	if k < 0 {
		return ""
	}
	sec := sheet[k:]
	if j := strings.Index(sec[1:], `data-muster-at-section="`); j >= 0 {
		sec = sec[:j+1]
	}
	return sec
}

var fix16aNameRe = regexp.MustCompile(`data-person-id="[^"]*"[^>]*>(?:<td>)?([^<]+?)\s*<`)

func fix16aNames(sec string) map[string]bool {
	out := map[string]bool{}
	for _, m := range fix16aNameRe.FindAllStringSubmatch(sec, -1) {
		out[strings.TrimSpace(m[1])] = true
	}
	return out
}

func fix16aExpect(t *testing.T, what string, got map[string]bool, want []string, not []string) {
	t.Helper()
	for _, n := range want {
		if !got[n] {
			t.Errorf("%s: %q missing (got %v)", what, n, got)
		}
	}
	for _, n := range not {
		if got[n] {
			t.Errorf("%s: %q listed, must not be", what, n)
		}
	}
}

// The moment muster places each person by the record as it stands: a
// corrected time, a hand-entered time, a turn-back, a run on the road, a
// person taken off a run by a correction, and someone who left before.
func TestFix16aMusterAtPlacesPeopleAtTheMoment(t *testing.T) {
	w := newFix16aWorld(t)
	body := fix16aMoment(t, w, "site-b", "10:00")
	if !strings.Contains(body, "as of 10:00 on 2026-09-29, Ice Camp B&#39;s clock") {
		t.Errorf("no 'as of' stamp naming the post's clock")
	}
	onSite := fix16aSection(t, body, "site-b", "on-site")
	fix16aExpect(t, "on site at 10:00", fix16aNames(onSite),
		[]string{"Able, Ann", "Cole, Cal", "Evans, Eve", "Hill, Hal"},
		[]string{"Baker, Ben", "Dunn, Dee", "Fox, Fay", "Gray, Gus", "Ives, Ivy", "Kerr, Kim", "Ashton, Joss"})
	// Grouped like the live board: company, then trade.
	if !strings.Contains(onSite, `data-company-group="Alpha Drilling"`) || !strings.Contains(onSite, `data-company-group="Beta Haul"`) {
		t.Errorf("on-site names are not grouped by company:\n%s", onSite)
	}
	for name, mark := range map[string]string{"Cole, Cal": "time in corrected by hand", "Evans, Eve": "time in entered by hand"} {
		if !regexp.MustCompile(regexp.QuoteMeta(name) + `[^\n]*?` + regexp.QuoteMeta(mark)).MatchString(onSite) {
			t.Errorf("%s lacks the mark %q", name, mark)
		}
	}
	road := fix16aSection(t, body, "site-b", "on-the-road")
	fix16aExpect(t, "on the road at 10:00", fix16aNames(road), []string{"Fox, Fay", "Gray, Gus"}, []string{"Hill, Hal", "Ives, Ivy"})
	if !strings.Contains(road, "TRK-12") || !strings.Contains(road, "on the way to Ice Pad") {
		t.Errorf("the run is not described:\n%s", road)
	}
	gaps := fix16aSection(t, body, "site-b", "not-tracked")
	fix16aExpect(t, "not tracked at 10:00", fix16aNames(gaps), []string{"Ives, Ivy"}, nil)
	if !strings.Contains(gaps, "taken off this run by a trip correction") {
		t.Errorf("Ivy's row does not say why:\n%s", gaps)
	}
	unplaced := fix16aSection(t, body, "site-b", "unplaced")
	fix16aExpect(t, "could not be placed at 10:00", fix16aNames(unplaced), []string{"Kerr, Kim"}, nil)
	if !strings.Contains(unplaced, "uncertain") {
		t.Errorf("Kim's row does not say why:\n%s", unplaced)
	}

	// Before the turn-back, Hal is on the road on TRK-20, not on site.
	body = fix16aMoment(t, w, "site-b", "09:40")
	fix16aExpect(t, "on the road at 09:40", fix16aNames(fix16aSection(t, body, "site-b", "on-the-road")),
		[]string{"Hill, Hal", "Fox, Fay", "Gray, Gus"}, nil)
	fix16aExpect(t, "on site at 09:40", fix16aNames(fix16aSection(t, body, "site-b", "on-site")),
		[]string{"Able, Ann", "Cole, Cal", "Evans, Eve"}, []string{"Hill, Hal", "Baker, Ben"})
	if !strings.Contains(body, "later turned back") {
		t.Errorf("TRK-20 at 09:40 does not say it later turned back")
	}

	// Before Ben's hand-typed time out he is on site, marked.
	body = fix16aMoment(t, w, "site-b", "08:30")
	on := fix16aSection(t, body, "site-b", "on-site")
	fix16aExpect(t, "on site at 08:30", fix16aNames(on), []string{"Baker, Ben", "Fox, Fay", "Hill, Hal"}, []string{"Cole, Cal", "Evans, Eve"})
	if !strings.Contains(on, "time out entered by hand") {
		t.Errorf("Ben's hand-typed time out is not marked")
	}

	// After TRK-12 arrives, Fay and Gus are on site at Ice Pad.
	body = fix16aMoment(t, w, "site-a", "11:30")
	fix16aExpect(t, "Ice Pad at 11:30", fix16aNames(fix16aSection(t, body, "site-a", "on-site")),
		[]string{"Ashton, Joss", "Fox, Fay", "Gray, Gus"}, []string{"Ives, Ivy"})
}

// Last seen: across posts — the sign-in at the far post, the run there.
func TestFix16aLastSeenAcrossPosts(t *testing.T) {
	w := newFix16aWorld(t)
	code, body := fix16aGet(t, w.srv, "", "/last-seen", url.Values{"person": {w.gus.PersonID}}, true)
	if code != http.StatusOK {
		t.Fatalf("GET /last-seen = %d", code)
	}
	sec := func(k string) string {
		m := regexp.MustCompile(`(?s)data-last-seen="` + k + `">(.*?)</dd>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no %s row:\n%s", k, body)
		}
		return m[1]
	}
	if s := sec("sign-in"); !strings.Contains(s, "Ice Pad") || !strings.Contains(s, "2026-09-29 11:00 (Ice Pad&#39;s clock)") || !strings.Contains(s, "ago") {
		t.Errorf("last sign-in is not Ice Pad 11:00:\n%s", s)
	}
	if s := sec("sign-out"); !strings.Contains(s, "Ice Camp B") || !strings.Contains(s, "left on a truck") {
		t.Errorf("last sign-out is not leaving Ice Camp B on a truck:\n%s", s)
	}
	trip := sec("trip")
	for _, want := range []string{"TRK-12", "Ice Camp B", "Ice Pad", "2026-09-29 09:30 (Ice Camp B&#39;s clock)", "Fox, Fay", "arrived"} {
		if !strings.Contains(trip, want) {
			t.Errorf("last trip lacks %q:\n%s", want, trip)
		}
	}
	if strings.Contains(trip, "Ives, Ivy") {
		t.Errorf("the last trip lists Ivy, taken off it by correction")
	}
	if !strings.Contains(sec("now"), "on site at Ice Pad") {
		t.Errorf("now is not on site at Ice Pad: %s", sec("now"))
	}
	if !strings.Contains(body, "(907) 555-0007") {
		t.Errorf("the phone is not shown in full to a logged-in user")
	}
	// Joss: only ever at the peer post.
	_, body = fix16aGet(t, w.srv, "", "/last-seen", url.Values{"person": {w.joss.PersonID}}, true)
	if !strings.Contains(body, "on site at Ice Pad") || !strings.Contains(body, "no trip on record here") {
		t.Errorf("Joss's last-seen is wrong:\n%s", body)
	}
	// An unknown id says so.
	_, body = fix16aGet(t, w.srv, "", "/last-seen", url.Values{"person": {"nobody"}}, true)
	if !strings.Contains(body, `id="last-seen-unknown"`) {
		t.Errorf("an unknown person does not say NOT FOUND")
	}
}

// Anonymous: the login form and nothing else, on both pages.
func TestFix16aRequireLogin(t *testing.T) {
	w := newFix16aWorld(t)
	for _, c := range []struct {
		path string
		q    url.Values
	}{
		{"/muster/at", url.Values{"site": {"site-b"}, "date": {"2026-09-29"}, "time": {"10:00"}}},
		{"/last-seen", url.Values{"person": {w.gus.PersonID}}},
	} {
		code, body := fix16aGet(t, w.srv, "", c.path, c.q, false)
		if code != http.StatusOK || !strings.Contains(body, `name="password"`) {
			t.Fatalf("anonymous GET %s = %d, want the login form", c.path, code)
		}
		for _, leak := range []string{"Able", "Gray", "0007", "TRK-12", "data-muster-at-site", "data-last-seen"} {
			if strings.Contains(body, leak) {
				t.Errorf("anonymous GET %s leaks %q", c.path, leak)
			}
		}
	}
}

// On the dispatch host: both pages read, link only to dispatch-allowed
// pages, offer "All sites", and every write is still refused.
func TestFix16aDispatchHostIsReadOnly(t *testing.T) {
	w := newFix16aWorld(t)
	allowed := func(h string) bool {
		for _, p := range []string{"/analytics", "/search", "/theme", "/muster/at", "/last-seen"} {
			if strings.HasPrefix(h, p) {
				return true
			}
		}
		return false
	}
	code, body := fix16aGet(t, w.srv, fix7DispatchHost, "/muster/at", url.Values{"site": {"all"}, "date": {"2026-09-29"}, "time": {"10:00"}}, true)
	if code != http.StatusOK {
		t.Fatalf("GET /muster/at on the dispatch host = %d", code)
	}
	if !strings.Contains(body, `<option value="all" selected>All sites</option>`) {
		t.Errorf("the dispatch host does not offer All sites")
	}
	fix16aExpect(t, "all sites: Ice Pad", fix16aNames(fix16aSection(t, body, "site-a", "on-site")), []string{"Ashton, Joss"}, nil)
	fix16aExpect(t, "all sites: Ice Camp B", fix16aNames(fix16aSection(t, body, "site-b", "on-site")), []string{"Able, Ann"}, nil)
	_, ls := fix16aGet(t, w.srv, fix7DispatchHost, "/last-seen", url.Values{"person": {w.gus.PersonID}}, true)
	for _, page := range []string{body, ls} {
		if strings.Contains(page, "/trips") || strings.Contains(page, `href="/gate`) || strings.Contains(page, `href="/muster"`) {
			t.Errorf("a dispatch-host page links into the operator console")
		}
		for _, href := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(page, -1) {
			if !allowed(href[1]) {
				t.Errorf("a dispatch-host page links to %q", href[1])
			}
		}
	}
	for _, p := range []string{"/muster/at", "/last-seen"} {
		if rec := fix7Do(t, w.srv, "POST", fix7DispatchHost, p, url.Values{"x": {"y"}}); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s on the dispatch host = %d, want 403", p, rec.Code)
		}
	}
	// A post host does not offer All sites (pick a site).
	_, post := fix16aGet(t, w.srv, "", "/muster/at", nil, true)
	if strings.Contains(post, `value="all"`) {
		t.Errorf("a post host offers All sites")
	}
	if code, _ := fix16aGet(t, w.srv, "", "/muster/at", url.Values{"site": {"all"}, "date": {"2026-09-29"}, "time": {"10:00"}}, true); code != http.StatusOK {
		t.Errorf("site=all on a post = %d", code)
	}
}

// The links: the muster page offers "Muster at a time…"; search results,
// the moment's names and the live muster's Not tracked rows open last seen.
func TestFix16aLinks(t *testing.T) {
	w := newFix16aWorld(t)
	_, muster := fix16aGet(t, w.srv, "", "/muster", nil, true)
	if !strings.Contains(muster, `href="/muster/at" data-muster-at-link`) {
		t.Errorf("/muster has no Muster at a time link")
	}
	nt := sectionBody(muster, "not-tracked")
	if !strings.Contains(nt, `href="/last-seen?person=`+w.ivy.PersonID+`"`) {
		t.Errorf("the live muster's Not tracked row has no last-seen link:\n%s", nt)
	}
	_, search := fix16aGet(t, w.srv, "", "/search", url.Values{"last": {"gray"}}, true)
	if !strings.Contains(search, `href="/last-seen?person=`+w.gus.PersonID+`"`) {
		t.Errorf("search results have no last-seen link")
	}
	body := fix16aMoment(t, w, "site-b", "10:00")
	for _, p := range []domain.Person{w.ann, w.gus, w.ivy, w.kim} {
		if !strings.Contains(body, `href="/last-seen?person=`+p.PersonID+`"`) {
			t.Errorf("the moment muster has no last-seen link for %s", p.Name)
		}
	}
}

// 320 px: nothing on the two pages forces a width; every table scrolls
// inside its own region.
func TestFix16aFitsAPhone(t *testing.T) {
	w := newFix16aWorld(t)
	body := fix16aMoment(t, w, "site-b", "10:00")
	_, ls := fix16aGet(t, w.srv, "", "/last-seen", url.Values{"person": {w.gus.PersonID}}, true)
	for name, page := range map[string]string{"muster at": body, "last seen": ls} {
		css := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindAllStringSubmatch(page, -1)
		own := css[len(css)-1][1] // the page's own block follows the shared one
		for _, m := range regexp.MustCompile(`[{;]\s*(?:min-)?width:\s*(\d+(?:\.\d+)?)(px|rem)`).FindAllStringSubmatch(own, -1) {
			if n, _ := strconv.ParseFloat(m[1], 64); m[2] == "px" && n > 320 || m[2] == "rem" && n > 20 {
				t.Errorf("%s: a fixed width %s%s in the page's own style", name, m[1], m[2])
			}
		}
		tables := strings.Count(page, "<table")
		wrapped := strings.Count(page, `<div class="table-wrap"`)
		if tables > wrapped {
			t.Errorf("%s: %d tables, only %d in a scrolling region", name, tables, wrapped)
		}
	}
}

// The page's "Download CSV" carries exactly the names the page lists,
// through writeCSVDownload (BOM, attachment); login-only; dispatch-readable.
func TestFix16aMusterAtCSV(t *testing.T) {
	w := newFix16aWorld(t)
	q := url.Values{"site": {"site-b"}, "date": {"2026-09-29"}, "time": {"10:00"}}
	body := fix16aMoment(t, w, "site-b", "10:00")
	if !strings.Contains(body, `href="/muster/at.csv?`+html.EscapeString(q.Encode())+`" data-muster-at-csv`) {
		t.Fatalf("the page has no Download CSV link for this moment")
	}
	for _, host := range []string{"", fix7DispatchHost} {
		req := httptest.NewRequest("GET", "/muster/at.csv?"+q.Encode(), nil)
		if host != "" {
			req.Host = host
		}
		req.AddCookie(loginCookie(t, w.srv, testLoginPassword))
		rec := httptest.NewRecorder()
		w.srv.Root().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") ||
			!strings.Contains(rec.Header().Get("Content-Disposition"), "muster-at-site-b-20260929-1000") {
			t.Fatalf("host %q: GET /muster/at.csv = %d %v", host, rec.Code, rec.Header())
		}
		csv := rec.Body.String()
		if !strings.HasPrefix(csv, "\ufeffas_of,post,section,person,company,trade,detail,marks\r\n") {
			t.Errorf("no BOM + header: %q", csv[:min(80, len(csv))])
		}
		for _, want := range []string{
			"2026-09-29 10:00,Ice Camp B,on site,\"Able, Ann\",Alpha Drilling",
			"on site,\"Evans, Eve\",Beta Haul",
			"on the road,\"Gray, Gus\"",
			"not tracked,\"Ives, Ivy\"",
			"could not be placed,\"Kerr, Kim\"",
		} {
			if !strings.Contains(csv, want) {
				t.Errorf("host %q: CSV lacks %q:\n%s", host, want, csv)
			}
		}
		if strings.Contains(csv, "Baker, Ben") || strings.Contains(csv, "Dunn, Dee") {
			t.Errorf("the CSV lists someone the page does not")
		}
	}
	if code, b := fix16aGet(t, w.srv, "", "/muster/at.csv", q, false); code != http.StatusOK || !strings.Contains(b, `name="password"`) || strings.Contains(b, "Able") {
		t.Errorf("anonymous GET /muster/at.csv = %d, want the login form only", code)
	}
	if code, _ := fix16aGet(t, w.srv, "", "/muster/at.csv", url.Values{"site": {"site-b"}}, true); code != http.StatusBadRequest {
		t.Errorf("no moment = %d, want 400", code)
	}
}
