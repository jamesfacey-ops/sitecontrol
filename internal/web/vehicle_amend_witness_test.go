// vehicle_amend_witness_test.go — witness phase (row Y), y4-vehweb.
//
// The census (U-row-Y-census.md) found handleVehicleAmend and
// handleVehicleCreate FUNC-NEVER-ENTERED: 0 of 13 blocks reached by any
// test, and both routes are LIVE (`POST /vehicle/amend`, `POST
// /vehicle/create`, registered in registerVehicleAmendRoutes, behind
// requireLogin). Plants Q12 and Q12b prove it — an unconditional HTTP 500
// inserted at the top of each handler leaves the whole suite unchanged.
//
// This file drives both routes end to end through srv.Handler() (never
// calling the handler function directly — a witness must exercise the real
// mux, the real requireLogin gate and the real template, the same as an
// officer's browser would) and asserts the officer-facing outcome: the
// record changes, the PRG redirect shape, and the exact authored refusal
// sentence when a plant would otherwise remove or corrupt it.
package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newVehicleAmendTestServer is a bare site-a box: registered domain
// projections and a login password, nothing else. Vehicle amend/create do
// not need a company, a trade or a person roster — unlike gate_test.go's
// newGateTestServer, which exists to drive check-in.
func newVehicleAmendTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st
}

// --- Q12: POST /vehicle/amend — the enormous cheap kill ---------------------

// TestWitnessQ12VehicleAmendPostAmendsARealVehicle drives the real route with
// a real, previously-created vehicle and asserts the officer's change lands:
// a 303 PRG redirect carrying the confirmation key, and the vehicle's role
// actually changed in the store. Under plant Q12 (an unconditional 500 at
// the top of handleVehicleAmend) this fails at the very first assertion —
// the redirect never happens and nothing is amended.
func TestWitnessQ12VehicleAmendPostAmendsARealVehicle(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	num := "TRK-1"
	v, err := domain.CreateVehicle(st, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	rec := postForm(t, srv, "/vehicle/amend", url.Values{
		"vehicle_id": {v.VehicleID},
		"role":       {"Vac Truck"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /vehicle/amend (real amendment) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/vehicle/amend?recorded=") {
		t.Fatalf("redirect Location = %q, want the PRG confirmation shape /vehicle/amend?recorded=<key>", loc)
	}

	got, err := domain.VehicleByID(st, v.VehicleID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("vehicle %s vanished after amendment", v.VehicleID)
	}
	if got.Role == nil || *got.Role != "Vac Truck" {
		t.Fatalf("vehicle %s role after amendment = %v, want \"Vac Truck\" — the POST did not actually change the record", v.VehicleID, got.Role)
	}
	if got.Number == nil || *got.Number != "TRK-1" {
		t.Errorf("vehicle %s number after amendment = %v, want the original \"TRK-1\" untouched", v.VehicleID, got.Number)
	}
}

// --- Q12b: POST /vehicle/create — the second enormous cheap kill -----------

// TestWitnessQ12bVehicleCreatePostCreatesARealVehicle drives the real route
// and asserts a new vehicle actually exists afterward, reached the way an
// officer reaches it: the 303 redirect to /vehicle/amend?created=<id>, and
// that id resolving to a real record with the typed number. Under plant
// Q12b this fails immediately — no redirect, no record.
func TestWitnessQ12bVehicleCreatePostCreatesARealVehicle(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)

	rec := postForm(t, srv, "/vehicle/create", url.Values{
		"number": {"TRK-99"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /vehicle/create (real creation) = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	const prefix = "/vehicle/amend?created="
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("redirect Location = %q, want the confirmation shape %s<id>", loc, prefix)
	}
	// ⟦D23, fix2-fj 2026-10-04⟧ the redirect now ends in the notice seal
	// ("&ns=…", noticeseal.go); the id is the value up to it.
	id, _, _ := strings.Cut(strings.TrimPrefix(loc, prefix), "&")
	if id == "" {
		t.Fatalf("redirect Location %q carries no vehicle id", loc)
	}

	got, err := domain.VehicleByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("vehicle %s (from the redirect) does not exist — the POST did not actually create a record", id)
	}
	if got.Number == nil || *got.Number != "TRK-99" {
		t.Fatalf("created vehicle %s number = %v, want \"TRK-99\"", id, got.Number)
	}
}

// --- outward from the kill: officer-facing refusal sentences ---------------
//
// The census found 33 refusal sentences across the system that no test has
// ever rendered. handleVehicleAmend's and handleVehicleCreate's own ERR-ARM
// (vehicle_amend.go:283-286 and :322-325, both FUNC-NEVER-ENTERED) is the
// dispatch to renderVehicleAmendError, which is what puts one of those
// sentences on the page. Rendering them — asserting what the officer reads,
// not the Go error value — is the deliverable the brief asks for.

// TestVehicleAmendRefusalArmsRenderTheExactOfficerSentence exercises several
// of domain.AmendVehicle's PRE-APPEND refusal arms through the real POST and
// asserts: HTTP 409 (never a redirect that would look like success to the
// officer), the exact authored sentence on the page, and that nothing was
// appended to the store (the vehicle is unchanged).
func TestVehicleAmendRefusalArmsRenderTheExactOfficerSentence(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{
			// Arm 1: an amendment that sets nothing and clears nothing.
			name: "no-op",
			form: url.Values{},
			want: "the amendment changes nothing (no field set, nothing cleared)",
		},
		{
			// Arm 3: the placeholder guard, at the amendment door.
			name: "placeholder-number",
			form: url.Values{"number": {"walk-in"}},
			want: `"walk-in" does not identify a vehicle`,
		},
		{
			// Arm 7: the trivial self-loop.
			name: "self-same-as",
			form: url.Values{}, // same_as filled in per-case below
			want: "a record cannot be merged with itself",
		},
		{
			// Arm 8: an unknown same_as target.
			name: "unknown-same-as-target",
			form: url.Values{"same_as": {"vehicle-does-not-exist"}},
			want: `"vehicle-does-not-exist" is not a record this post has ever been told about`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, st := newVehicleAmendTestServer(t)
			num := "TRK-2"
			v, err := domain.CreateVehicle(st, "site-a", 1, &num, nil, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"vehicle_id": {v.VehicleID}}
			for k, vals := range tc.form {
				form[k] = vals
			}
			if tc.name == "self-same-as" {
				form.Set("same_as", v.VehicleID)
			}

			rec := postForm(t, srv, "/vehicle/amend", form)
			if rec.Code != http.StatusConflict {
				t.Fatalf("POST /vehicle/amend (%s) = %d, want 409: %s", tc.name, rec.Code, rec.Body.String())
			}
			body := html.UnescapeString(rec.Body.String())
			if !strings.Contains(body, tc.want) {
				t.Errorf("refusal page does not carry the authored sentence.\nwant substring: %s\ngot body:\n%s", tc.want, body)
			}
			if !strings.Contains(body, "REFUSED") {
				t.Errorf("refusal page missing the REFUSED banner:\n%s", body)
			}
			// u1b-web §3.4: both {{if .Error}} and {{if .InputError}} render
			// "REFUSED — <sentence>", so co-presence of the REFUSED banner and
			// the wanted sentence does not by itself prove these are DOMAIN
			// refusals (typed.Error) rather than input errors (typed.InputError)
			// — only the InputError notice adds this class-(b) guidance. Produced:
			// routing VehicleAmendRefusal through typed.InputError at
			// vehicle_amend.go:337-338 leaves all four subcases above passing.
			// Assert the guidance is ABSENT, the mirror of what
			// TestVehicleAmendInputErrorRendersOnAMistypedID already asserts
			// PRESENT for the InputError copy.
			if strings.Contains(body, "Check the vehicle id against the list") {
				t.Errorf("a DOMAIN refusal (%s) rendered the InputError class-(b) guidance — this is a mistyped-id sentence pointing at a record the officer never named as an id:\n%s", tc.name, body)
			}

			after, err := domain.VehicleByID(st, v.VehicleID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Number == nil || *after.Number != "TRK-2" {
				t.Errorf("a REFUSED amendment changed the record: number = %v, want untouched \"TRK-2\"", after.Number)
			}
		})
	}
}

// TestVehicleAmendInputErrorRendersOnAMistypedID is W7-15's class (b): the
// officer named a record this post cannot act on. It must be a 409 with the
// domain's own sentence in .InputError — never the internal-fault arm (that
// miscarriage, sending a typo to "radio it through" at HTTP 500, is what
// blocked W5d's commit and is exactly what this route's file comment warns
// against for its own surface).
func TestVehicleAmendInputErrorRendersOnAMistypedID(t *testing.T) {
	cases := []struct {
		name      string
		vehicleID string
		want      string
	}{
		{name: "empty-id", vehicleID: "", want: "a vehicle id is required"},
		{name: "unknown-id", vehicleID: "vehicle-nope", want: `"vehicle-nope" is not a vehicle on this post`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newVehicleAmendTestServer(t)
			rec := postForm(t, srv, "/vehicle/amend", url.Values{
				"vehicle_id": {tc.vehicleID},
				"role":       {"Vac Truck"},
			})
			if rec.Code != http.StatusConflict {
				t.Fatalf("POST /vehicle/amend (%s) = %d, want 409: %s", tc.name, rec.Code, rec.Body.String())
			}
			body := html.UnescapeString(rec.Body.String())
			if !strings.Contains(body, tc.want) {
				t.Errorf("input-error page does not carry the authored sentence.\nwant substring: %s\ngot body:\n%s", tc.want, body)
			}
			if !strings.Contains(body, "Check the vehicle id against the list") {
				t.Errorf("input-error page missing the class-(b) guidance copy (never the internal-fault copy):\n%s", body)
			}
			if strings.Contains(body, "radio it through") && strings.Contains(body, "NOT RECORDED") {
				t.Errorf("a mistyped id fell through to the internal-fault arm — the exact miscarriage W7-15 exists to prevent:\n%s", body)
			}
		})
	}
}

// TestVehicleCreateRefusalRendersTheExactOfficerSentence covers
// domain.CreateVehicle's two refusal arms — neither number nor role typed,
// and the placeholder guard at the OTHER typing moment — through the real
// POST /vehicle/create.
func TestVehicleCreateRefusalRendersTheExactOfficerSentence(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{
			name: "neither-number-nor-role",
			form: url.Values{},
			want: "a vehicle record needs a number or a role",
		},
		{
			name: "placeholder-number",
			form: url.Values{"number": {"n/a"}},
			want: `"n/a" does not identify a vehicle`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, st := newVehicleAmendTestServer(t)
			before, err := domain.Vehicles(st)
			if err != nil {
				t.Fatal(err)
			}

			rec := postForm(t, srv, "/vehicle/create", tc.form)
			if rec.Code != http.StatusConflict {
				t.Fatalf("POST /vehicle/create (%s) = %d, want 409: %s", tc.name, rec.Code, rec.Body.String())
			}
			body := html.UnescapeString(rec.Body.String())
			if !strings.Contains(body, tc.want) {
				t.Errorf("refusal page does not carry the authored sentence.\nwant substring: %s\ngot body:\n%s", tc.want, body)
			}

			after, err := domain.Vehicles(st)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Errorf("a REFUSED creation still minted a record: %d vehicles before, %d after", len(before), len(after))
			}
		})
	}
}

// TestBuildVehicleOutcomeViewFailsClosedOnAnUnknownRecordedKey reaches
// buildVehicleOutcomeView itself (census: FUNC-NEVER-ENTERED, 0 of 8 blocks)
// through the real GET /vehicle/amend?recorded=<key>. domain.Outcome
// documents a key matching no event as an ERROR, never an empty success —
// this asserts the officer actually sees the "could not be determined"
// notice rather than a silent, confident-looking page.
func TestBuildVehicleOutcomeViewFailsClosedOnAnUnknownRecordedKey(t *testing.T) {
	srv, _ := newVehicleAmendTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)

	// ⟦D23, fix2-fj 2026-10-04⟧ James, 2026-10-04 (FINDINGS-R4 cluster J): ?recorded= renders only off a sealed redirect, so this arm is reached through one; a hand-typed value renders nothing (fix2_fj_sealed_notices_test.go).
	req := httptest.NewRequest("GET", sealNotices("/vehicle/amend?recorded=site-a.1.999999"), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /vehicle/amend?recorded=<unknown key> = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, "outcome could not be determined") {
		t.Errorf("page does not carry the Failed-closed notice for an unknown recorded key:\n%s", body)
	}
}
