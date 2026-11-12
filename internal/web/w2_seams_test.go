// w2_seams_test.go — THE ONE place W2's web tests bind every Go/HTTP
// spelling the LOCKED criteria leave undetermined
// (design/WINDOW-BUILD-PLAN-v1.md §3 W2, criteria LOCKED 2026-08-28 —
// rev-3 addendum folded; test-writer note per plan §5 step 2).
//
// SHAPE PRESUMPTIONS, stated for the gate (§5 step 3). The criteria fix the
// BEHAVIOUR (an explicit posted signal, TP §4.5; bounds per TP §4.6; the
// served control per W2-11) but not these spellings. Each is bound HERE,
// once, so the builder can be told and a dispute goes to the gate rather
// than into nine divergent tests:
//
//   - SEAM W2-S-A: the posted wall-string field is named "occurred_at"
//     (w2OccurredAtField). TP §4.5 fixes only time_source's name.
//   - SEAM W2-S-B: the wall string is the datetime-local wire format
//     "2006-01-02T15:04", interpreted in the BOX's local zone (site-local
//     per TP §4.1; the box's zone is the site's zone), stored as UTC ms —
//     w2Wall / w2ParseWall are the one round-trip definition.
//   - SEAM W2-S-C: a bounds warning is recognisable in served markup by the
//     class token "time-warning" (w2TimeWarnToken) on the notice element,
//     and it renders on the page the operator sees NEXT (the POST's own
//     200/4xx body, or the page a 303 redirects to, same session). The
//     records-floor warning's copy contains "predates" — that word is TP
//     §4.6 rule 2's own quoted copy, not a seam.
//   - SEAM W2-S-D: the two bounds constants are named test-side constants
//     (S-5's instruction: named constants, never magic numbers, so a
//     re-valuing is one edit): w2FutureSkewAllowanceMS = 5 minutes,
//     w2AbsurdAgeHorizonMS = 30 days. Both are TP §8 Q4 proposed defaults.
//   - SEAM W2-S-E: the served time control (W2-11) is asserted as: the
//     form's untouched JS-off submission carries time_source=default; the
//     form markup contains a datetime-local input (the openable control)
//     and the "change time" affordance text (case-insensitive). The two
//     phrases are TP §4.5's own words; the exact markup around them is not
//     prescribed.
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	// The explicit signal (TP §4.5 — this name is DOC-fixed, not a seam).
	w2TimeSourceField = "time_source"
	// SEAM W2-S-A.
	w2OccurredAtField = "occurred_at"
	// SEAM W2-S-B: datetime-local's wire format (what a browser posts).
	w2WallFmt = "2006-01-02T15:04"
	// SEAM W2-S-C.
	w2TimeWarnToken = "time-warning"
	// TP §4.6 rule 2's own quoted warning copy ("this time predates this
	// box's records") — asserted by its load-bearing word.
	w2RecordsFloorPhrase = "predates"
	// TP §4.5's affordance, asserted case-insensitively.
	w2AffordancePhrase = "change time"
	// SEAM W2-S-D (TP §8 Q4 proposed defaults; S-5).
	w2FutureSkewAllowanceMS = int64(5 * 60 * 1000)
	w2AbsurdAgeHorizonMS    = int64(30 * 24 * 60 * 60 * 1000)
)

// w2Base is the fixed audit-clock "now" for W2 fixtures: 1.8e12 ms is a
// whole minute (divisible by 60000), so datetime-local round-trips exactly,
// and it is far enough ahead of the real clock that fixture rows authored
// under it never race the wall clock.
const w2Base = int64(1_800_000_000_000)

const w2Minute = int64(60_000)
const w2Hour = int64(3_600_000)
const w2Day = int64(86_400_000)

// w2Reading builds a fixed audit-clock reading (w1Reading's twin — that
// helper lives in internal/store's tests and is not importable here).
func w2Reading(wallMS int64, state store.ClockState) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: state}
	}
}

// w2Wall renders a UTC-ms instant as the datetime-local wall string a
// browser at this box would post (SEAM W2-S-B). Callers pass whole-minute
// values so w2ParseWall(w2Wall(ms)) == ms exactly.
func w2Wall(ms int64) string {
	return time.UnixMilli(ms).In(time.Local).Format(w2WallFmt)
}

// w2ParseWall is the ONE test-side definition of the wall-string parse the
// production capture must agree with (SEAM W2-S-B): site-local wall string
// in, UTC ms out.
func w2ParseWall(t *testing.T, s string) int64 {
	t.Helper()
	tt, err := time.ParseInLocation(w2WallFmt, s, time.Local)
	if err != nil {
		t.Fatalf("w2ParseWall(%q): %v", s, err)
	}
	return tt.UnixMilli()
}

// --- store observation -------------------------------------------------

// w2Watermark reads the own-stream watermark, fatally.
func w2Watermark(t *testing.T, st *store.Store, siteID string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// w2EventsFrom reads every stored own-stream event with Seq > afterSeq.
func w2EventsFrom(t *testing.T, st *store.Store, siteID string, epoch, afterSeq int64) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, epoch, afterSeq+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

var w2TypeRe = regexp.MustCompile(`"type":"([a-z_]+)"`)

// w2PayloadType extracts the event type from a stored payload.
func w2PayloadType(t *testing.T, env store.Envelope) string {
	t.Helper()
	m := w2TypeRe.FindStringSubmatch(string(env.Payload))
	if m == nil {
		t.Fatalf("stored payload carries no type: %s", env.Payload)
	}
	return m[1]
}

// w2EventsOfType filters envelopes by payload type.
func w2EventsOfType(t *testing.T, envs []store.Envelope, typ string) []store.Envelope {
	t.Helper()
	var out []store.Envelope
	for _, e := range envs {
		if w2PayloadType(t, e) == typ {
			out = append(out, e)
		}
	}
	return out
}

// --- HTTP with a kept session ------------------------------------------

// w2PostForm posts like postForm but returns the session cookie too, so a
// follow-up GET can see anything the accept path queued for the SAME
// operator (SEAM W2-S-C's "next page" rule).
func w2PostForm(t *testing.T, srv *Server, path string, form url.Values) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	cookie := loginCookie(t, srv, testLoginPassword)
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec, cookie
}

// w2Get fetches a path with the given session cookie (nil = anonymous).
func w2Get(t *testing.T, srv *Server, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// w2NextOperatorPage is what the operator reads after a POST: the POST's
// own rendered body, or — on a redirect — the redirected-to page fetched
// with the same session (SEAM W2-S-C).
func w2NextOperatorPage(t *testing.T, srv *Server, rec *httptest.ResponseRecorder, cookie *http.Cookie) string {
	t.Helper()
	if rec.Code == http.StatusSeeOther || rec.Code == http.StatusFound {
		loc := rec.Result().Header.Get("Location")
		if loc == "" {
			t.Fatalf("redirect (%d) with no Location", rec.Code)
		}
		next := w2Get(t, srv, loc, cookie)
		if next.Code != http.StatusOK {
			t.Fatalf("GET %s after the accept = %d: %s", loc, next.Code, next.Body.String())
		}
		return next.Body.String()
	}
	return rec.Body.String()
}

// w2StripNonMarkup removes style and script regions so a class token in a
// stylesheet cannot stand in for a rendered warning (this repo has been
// bitten by that twice — gate_form_test.go's file comment).
func w2StripNonMarkup(page string) string {
	return scriptRe.ReplaceAllString(styleRe.ReplaceAllString(page, ""), "")
}

// w2HasTimeWarning reports whether the operator-visible markup carries a
// bounds warning (SEAM W2-S-C).
func w2HasTimeWarning(page string) bool {
	return strings.Contains(w2StripNonMarkup(page), w2TimeWarnToken)
}

// --- fixture worlds ----------------------------------------------------

// w2Config is the site-a shape every W2 web fixture uses: one controlled
// peer, one uncontrolled pad (a fixture with neither would make destination
// assertions true by having nothing to choose from — newGateTestServer's
// own reasoning), and the shared demo login.
func w2Config(dbPath string) *config.Config {
	return &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: dbPath, SyncIntervalSeconds: 3600,
		LoginPassword: testLoginPassword,
		Peers:         []config.Peer{{SiteID: "site-b", BaseURL: "http://example.invalid"}},
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
			{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
		},
	}
}

// newW2Server is newGateTestServer with a CONTROLLED clock: the roster is
// authored at rosterClock so records-floor fixtures can place entered
// values above or below min(authored_at) deterministically, and the clock
// is left at w2Base/synced (override it per test).
func newW2Server(t *testing.T, rosterClockMS int64) (*Server, *store.Store, []domain.Person) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(rosterClockMS, store.ClockSynced)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Inspection", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Inspection", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var persons []domain.Person
	for _, name := range []string{"Dana Driver", "Pat Passenger", "Chris Crew"} {
		p, err := domain.CreatePerson(st, "site-a", 1, name, &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		persons = append(persons, p)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return New(w2Config(st.Path), st), st, persons
}

// newW2EmptyServer opens a server over a store with a genuinely EMPTY
// event_log (behaviour 5's fixture: no records, no floor). The clock sits
// at w2Base/synced.
func newW2EmptyServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w2-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return New(w2Config(st.Path), st), st
}

// w2CheckIn puts the named fixture people on site through the ordinary
// domain machinery (driver first), so pickers and check-out fixtures have
// open visits to work with.
func w2CheckIn(t *testing.T, st *store.Store, persons []domain.Person) {
	t.Helper()
	slots := make([]domain.PersonSlot, 0, len(persons))
	for _, p := range persons {
		slots = append(slots, domain.PersonSlot{ExistingID: p.PersonID})
	}
	if _, _, err := domain.GateCheckIn(st, "site-a", 1, domain.GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "CREW BUS", Driver: slots[0], Occupants: slots[1:],"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "CREW BUS"), Driver: slots[0], Occupants: slots[1:],
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

// w2LegacyCheckInBody is the measured stale-pre-bump body shape
// (gate.go:1275–1287's kept legacy form: driver slot + bare occupant
// arrays, NO passenger_list marker) — seam S-1's deep fixture.
func w2LegacyCheckInBody(vehicle, driverTyped string, occupantsTyped ...string) url.Values {
	vals := url.Values{}
	vals.Set("vehicle", vehicle)
	vals.Set("driver_typed", driverTyped)
	for _, o := range occupantsTyped {
		vals.Add("occupant_typed", o)
	}
	return vals
}
