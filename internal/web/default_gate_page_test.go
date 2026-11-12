// default_gate_page_test.go — James, 2026-09-10: "the default page for all
// sites other than the dispatch info only page should be the sign in and
// out page … increse the size of the onsite roster portion. the date of
// sign in should be displayed on the onsite roster as well." And, on whose
// clock the date is: "that is a moot point use the system clock".
//
// Four witnesses, all driven through the real mux (reports/witness/
// DEFAULT-GATE-PAGE.md):
//
//  1. GET / is the officer's front door — it sends them to /gate — and the
//     status page still exists, at /status, as a REGISTERED route (asserted
//     on the mux's matched pattern, never on a 200: the catch-all answers
//     200 to everything, web_test.go).
//  2. The catch-all still lands on the status page, and now SAYS the path
//     has no page on this post — measured at a road post's /orientation,
//     which is not registered there (orientation.go) and used to fall
//     through silently.
//  3. Every on-site row carries the sign-in time and date as recorded,
//     `hh:nn mm/dd` (time first, per James: "time/date"; 24-hour), and
//     nothing else — no clock caveat, no tooltip, no provenance marker.
//     Covered: a default-path check-in, an officer-typed one, a corrected
//     one (the correction is what shows), and an UNSOLICITED arrival
//     (visit.trip_id NULL — the other join path in domain.VisitSignInTimes).
//  4. A row recorded by a PREVIOUS box of this post (an earlier node_epoch,
//     recovered from the peer after a re-image) displays its date exactly
//     like a row this box recorded — same format, same markup.
//
// And one guard: the dates never order the roster (D-90/D-92). Three names
// whose sign-in times disagree with their alphabetical order, in both
// directions, still render alphabetically.
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fixedClock is a store.Clock that always reads `at` with `state` — how a
// test poses "this box's clock said X when it recorded this".
func fixedClock(at time.Time, state store.ClockState) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: at.UnixMilli(), MaxErrorUS: 0, State: state}
	}
}

// ⟦was: "signInLayoutUnderTest = \"15:04 01/02\""⟧ — the officer-facing
// form used to be ONE combined span, `hh:nn mm/dd`, 24-hour (James:
// "time/date"). GATE-ACCESS-STYLE (2026-09-12) split it into its own Time
// ("15:04") and Date ("01/02") columns — assertPlainDate below checks each
// against time.Format directly now, so nothing parses a combined layout any
// more.

// onSiteWhen reads the rendered roster TABLE (gate.go's buildRosterTableRows;
// GATE-ACCESS-STYLE, 2026-09-12 — Time and Date are now separate columns,
// no longer one combined "hh:nn mm/dd" span) into rosterNameKey(Last,
// First) → that row's Time/Date text and attributes.
//
// NONE OF THIS FILE'S FIXTURES AUTHOR domain.Person.LastName/FirstName
// (every domain.CreatePerson call below passes only the one combined
// Name), so rosterDisplayName's fallback always applies: Last is the
// WHOLE Name, First is blank (gate.go; D-48 forbids deriving a part FROM
// Name by parsing it — TestW5aNothingParsesAName enforces that on every
// production .go file). rosterNameKey therefore reduces to the plain
// Name for every row here, which is exactly why assertPlainDate below can
// key straight off the Name a person was created with, with no split of
// its own to keep in sync with gate.go's.
func onSiteWhen(t *testing.T, page string) map[string]onSiteWhenRow {
	t.Helper()
	out := map[string]onSiteWhenRow{}
	for _, r := range rosterRows(t, page, "checkout-picker") {
		out[rosterNameKey(r.last, r.first)] = onSiteWhenRow{
			time: r.timeText, date: r.dateText, timeAttrs: r.timeAttrs, dateAttrs: r.dateAttrs,
		}
	}
	return out
}

type onSiteWhenRow struct{ time, date, timeAttrs, dateAttrs string }

// rosterNameKey reconstructs one lookup string from a rendered row's Last
// and First cells — "Last, First" when both are present (an authored
// pair), or the bare Last when First is blank (rosterDisplayName's
// whole-Name fallback, gate.go).
func rosterNameKey(last, first string) string {
	if first == "" {
		return last
	}
	return last + ", " + first
}

func onSiteOrder(t *testing.T, page string) []string {
	t.Helper()
	var out []string
	for _, r := range rosterRows(t, page, "checkout-picker") {
		out = append(out, rosterNameKey(r.last, r.first))
	}
	return out
}

// assertPlainDate is the ruling in one place: the recorded time and date,
// each in the officer's own column, and nothing riding on either.
func assertPlainDate(t *testing.T, rows map[string]onSiteWhenRow, name string, at time.Time) {
	t.Helper()
	got, ok := rows[name]
	if !ok {
		t.Errorf("%s: no on-site row parsed", name)
		return
	}
	if wantTime := at.Format("15:04"); got.time != wantTime {
		t.Errorf("%s: sign-in time reads %q, want %q", name, got.time, wantTime)
	}
	if wantDate := at.Format("01/02"); got.date != wantDate {
		t.Errorf("%s: sign-in date reads %q, want %q", name, got.date, wantDate)
	}
	if strings.TrimSpace(got.timeAttrs) != "" || strings.TrimSpace(got.dateAttrs) != "" {
		t.Errorf("%s: the sign-in cells carry attributes (time=%q date=%q) — James: \"use the system clock\"; no tooltip, no provenance marker", name, got.timeAttrs, got.dateAttrs)
	}
	for _, marker := range []string{"(typed", "(corrected", "(clock", "PREVIOUS", "ago"} {
		if strings.Contains(got.time, marker) || strings.Contains(got.date, marker) {
			t.Errorf("%s: sign-in text carries the marker %q — provenance markers were ruled out", name, marker)
		}
	}
}

// --- 1. the front door -------------------------------------------------

func TestTheFrontDoorIsTheGateAndTheStatusPageLivesAtStatus(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	mux := srv.Handler().(*http.ServeMux)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/gate" {
		t.Fatalf("GET / = %d Location=%q, want 302 to /gate — the officer's sign in/out page is the default page (James, 2026-09-10); body:\n%.300s",
			rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	// The root is matched by the exact-root pattern, not by the catch-all.
	if _, pattern := mux.Handler(mustRequest(t, "GET", "/")); pattern != "GET /{$}" {
		t.Fatalf("GET / matched pattern %q, want the exact-root pattern \"GET /{$}\" — otherwise the redirect is the catch-all's and every unmatched path would bounce to the gate", pattern)
	}

	// The status page is a REGISTERED route at /status — asserted on the
	// matched pattern, because the catch-all would serve it at ANY path.
	if _, pattern := mux.Handler(mustRequest(t, "GET", "/status")); pattern != "GET /status" {
		t.Fatalf("GET /status matched pattern %q, want \"GET /status\" — the status page must keep an address of its own, not merely fall out of the catch-all", pattern)
	}
	body := rawPage(t, srv, "/status")
	for _, want := range []string{"db_path", "node_epoch", "schema_version", `<header class="topbar">`} {
		if !strings.Contains(body, want) {
			t.Errorf("/status is missing %q — it is not the status page", want)
		}
	}
	if strings.Contains(body, "no page at") {
		t.Errorf("/status carries the fall-through notice on its own registered address:\n%.400s", body)
	}
}

// --- 2. the fall-through --------------------------------------------------

func TestAnUnregisteredPathStillLandsOnTheStatusPageAndSaysSo(t *testing.T) {
	// A road post: orientation = false, so /orientation is NOT registered
	// (orientation.go) and falls through. Logged in, so the login gate is
	// not what answers.
	srv, _, _ := newOrientationServer(t, false)
	cookie := loginCookie(t, srv, testLoginPassword)
	rec := w2Get(t, srv, "/orientation", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("road post GET /orientation = %d, want 200 (the catch-all)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "db_path") {
		t.Fatalf("road post GET /orientation did not land on the status page:\n%.400s", body)
	}
	if !strings.Contains(body, "no page at") || !strings.Contains(body, "/orientation") {
		t.Errorf("the fall-through does not SAY that /orientation has no page on this post — an officer following a link from a pad's notice is left on a diagnostic page with no explanation:\n%.600s", body)
	}
	if strings.Contains(body, "data-orientation-section") || strings.Contains(body, "Held out at the gate") {
		t.Errorf("a road post served the held-out list on the fall-through (R-11 confines it to /orientation at an orientation post)")
	}

	// Any other unregistered path: same page, same sentence, the path named.
	body = rawPage(t, srv, "/no/such/page")
	if !strings.Contains(body, "db_path") || !strings.Contains(body, "no page at") || !strings.Contains(body, "/no/such/page") {
		t.Errorf("GET /no/such/page did not land on the status page with the path named:\n%.600s", body)
	}

	// Positive control: at an orientation post the same path IS a page,
	// and carries no such notice.
	pad, _, _ := newOrientationServer(t, true)
	padCookie := loginCookie(t, pad, testLoginPassword)
	if body := w2Get(t, pad, "/orientation", padCookie).Body.String(); strings.Contains(body, "no page at") || !strings.Contains(body, "data-orientation-section") {
		t.Errorf("at an orientation post /orientation is a registered page and must not carry the fall-through notice:\n%.400s", body)
	}
}

// --- 3. the sign-in date on every on-site row ------------------------------

func TestEveryOnSiteRowShowsTheRecordedSignInDatePlainly(t *testing.T) {
	srv, st, persons := newGateTestServer(t)
	now := time.Now()
	st.Clock = fixedClock(now, store.ClockSynced) // this box's system clock: "now"
	dana, pat, chris := persons[0], persons[1], persons[2]

	// Dana: default path — the system clock at the check-in.
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK 1"), Driver: domain.PersonSlot{ExistingID: dana.PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Pat: the officer typed a time three days back (the network was down).
	// The typed time is what was recorded, so it is what shows.
	threeDaysAgo := now.Add(-72 * time.Hour)
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK 2"), Driver: domain.PersonSlot{ExistingID: pat.PersonID},
	}, threeDaysAgo.UnixMilli(), "entered"); err != nil {
		t.Fatal(err)
	}
	// Chris: stamped now, then CORRECTED to yesterday (W4). The corrected
	// time is the record now, so it is what shows.
	_, chrisArrival, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK 3"), Driver: domain.PersonSlot{ExistingID: chris.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	yesterday := now.Add(-24 * time.Hour)
	if _, err := domain.CorrectOccurredAt(st, "site-a", 1, domain.TimeCorrection{
		Target:                store.Key{SiteID: chrisArrival.SiteID, NodeEpoch: chrisArrival.NodeEpoch, Seq: chrisArrival.Seq},
		CorrectedOccurredAtMS: yesterday.UnixMilli(), RecordedBy: "Officer Q",
	}); err != nil {
		t.Fatal(err)
	}
	// A fourth person arrives UNSOLICITED from the peer (visit.trip_id is
	// NULL — the other join path).
	co := companyIDNamed(t, srv, "Kakivik Asset Management")
	walker, err := domain.CreatePerson(st, "site-a", 1, "Walker, Uma", &co, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordUnsolicitedArrival(st, "site-a", 1, w7SeedVehicleID(t, st, "site-a", "ICE TRUCK 9"), "site-b", []string{walker.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	code, page := getGate(t, srv)
	if code != 200 {
		t.Fatalf("GET /gate = %d", code)
	}
	if !strings.Contains(page, "On site now (4)") {
		t.Fatalf("fixture: expected 4 on site:\n%s", page)
	}
	rows := onSiteWhen(t, page)
	if len(rows) != 4 {
		t.Fatalf("parsed %d on-site rows, want 4: %v", len(rows), rows)
	}
	assertPlainDate(t, rows, dana.Name, now)
	assertPlainDate(t, rows, pat.Name, threeDaysAgo)
	assertPlainDate(t, rows, chris.Name, yesterday)
	assertPlainDate(t, rows, walker.Name, now)
}

// --- 4. a row recorded by this post's PREVIOUS box displays like any other ----

func TestARowRecordedByThisPostsPreviousBoxDisplaysItsDateLikeAnyOther(t *testing.T) {
	a, b, _ := twoPosts(t)
	catering, err := domain.CreateCompany(b.st, "site-b", 1, "Camp Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	kitchen, err := domain.CreateTrade(b.st, "site-b", 1, "Kitchen", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crew := []domain.Person{
		person(t, b.st, "site-b", 1, "Silva, Marcos", catering.CompanyID, kitchen.TradeID),
		person(t, b.st, "site-b", 1, "Tran, Linh", catering.CompanyID, kitchen.TradeID),
	}

	// The Pad's FIRST box records the two walk-ins; its system clock reads
	// three days back (a fixed reading so the expected text is exact).
	now := time.Now()
	threeDaysAgo := now.Add(-72 * time.Hour)
	b.st.Clock = fixedClock(threeDaysAgo, store.ClockUnsynced)
	checkInAt(t, b, "gate walk-in", crew)
	settle(t, a, b) // Ice Road now holds the Pad's epoch-1 stream

	// The Pad is wiped and re-imaged: node_epoch 2, a fresh store, and
	// Rejoin (§8.6) pulls its own epoch-1 history back from Ice Road.
	b = wipeAndReimage(t, b)
	b.st.Clock = fixedClock(now, store.ClockSynced)
	b.eng.PullAll(context.Background())

	// And it records a walk-in of its own, under epoch 2.
	late := person(t, b.st, "site-b", 2, "Ulloa, Rosa", catering.CompanyID, kitchen.TradeID)
	n := "TRK-9"
	veh, err := domain.CreateVehicle(b.st, "site-b", 2, &n, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	trip, err := domain.DispatchLocalCheckIn(b.st, "site-b", 2, domain.DispatchInput{VehicleID: veh.VehicleID, DriverPersonID: late.PersonID}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(b.st, "site-b", 2, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	b.web.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/gate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /gate on the rebuilt Pad = %d:\n%s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if !strings.Contains(page, "On site now (3)") {
		t.Fatalf("the rebuilt Pad does not read 3 on site — own history not recovered, so the case is not posed:\n%s", page)
	}
	rows := onSiteWhen(t, page)
	// The recovered rows show the time their recording box's clock read,
	// in the same plain form as the row this box recorded — no marker.
	for _, p := range crew {
		assertPlainDate(t, rows, p.Name, threeDaysAgo)
	}
	assertPlainDate(t, rows, late.Name, now)
	if strings.Contains(pickerMarkup(t, page, "checkout-picker"), "PREVIOUS") {
		t.Errorf("the roster distinguishes a previous box's rows — ruled out (\"use the system clock\")")
	}
}

// --- the guard: dates never order the roster ----------------------------------

func TestSignInDatesNeverOrderTheOnSiteRoster(t *testing.T) {
	srv, st, _ := newGateTestServer(t)
	now := time.Now()
	st.Clock = fixedClock(now, store.ClockSynced)
	co := companyIDNamed(t, srv, "Kakivik Asset Management")
	// Alphabetical: Aaron, Mabel, Zed. By time ascending: Zed, Aaron, Mabel.
	// By time descending: Mabel, Aaron, Zed. Neither agrees with the names.
	for _, c := range []struct {
		name string
		ago  time.Duration
	}{{"Aaron, Ann", 48 * time.Hour}, {"Mabel, Mo", time.Hour}, {"Zed, Zoe", 72 * time.Hour}} {
		p, err := domain.CreatePerson(st, "site-a", 1, c.name, &co, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
			VehicleID: w7SeedVehicleID(t, st, "site-a", "V "+c.name), Driver: domain.PersonSlot{ExistingID: p.PersonID},
		}, now.Add(-c.ago).UnixMilli(), "entered"); err != nil {
			t.Fatal(err)
		}
	}
	_, page := getGate(t, srv)
	got := onSiteOrder(t, page)
	want := []string{"Aaron, Ann", "Mabel, Mo", "Zed, Zoe"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("on-site roster order %v, want alphabetical %v — a sign-in DATE is displayed, never sorted by (D-90/D-92: the clock orders nothing)", got, want)
	}
}
