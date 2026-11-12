// w7_routes_test.go — W7-31, the officer-facing routes: GATED, CLASSIFIED,
// and BOOKED.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// ⚠⚠ [[D-63]] MAKES THIS A CONDITION OF CLOSING, IN TERMS: "W7's lock must
// not close until the gating is an ASSERTED CRITERION with a MUTANT THAT REDS
// ON AN UNGUARDED ROUTE". This file is that assertion.
//
// ⚠⚠ THE AUTHORITY, AND THIS IS THE PART THAT MUST NOT BE GOT WRONG
// (`DR-W7-17`):
//
//   - **[[D-63]]** is cited for WHO MAY USE IT — "the officer, from normal
//     screens". That is James's ruling and IT NAMES VEHICLE.
//   - **[[D-12]]** is cited for NOT BEHIND A DEVELOPER SWITCH — "must not put
//     the fix behind `dev_routes`". That is James's ruling and IT NAMES
//     VEHICLE.
//   - **The `requireLogin` MECHANISM itself is `DR-W7-17`, a DESIGN RULING of
//     W7's lock**, made on PARITY OF BUILT SHAPE with `/person/amend` and
//     `/company/amend` [MEASURED — `person_amend.go:36-37`,
//     `company_amend.go:65-66`].
//   - ⚠⚠ **IT CITES NEITHER [[D-51]] NOR [[D-46]]**, and the prohibition is
//     the ruling's own subject: neither names vehicle, and **D-51's whole
//     existence is a ruling that this carry-across needs its own authority**,
//     so citing it here would be committing the defect AT THE SITE OF ITS OWN
//     PROHIBITION.
//   - ⚠ AND THE **ERRATUM to [[D-63]]** APPLIES: the reason vehicle needs its
//     own authority is NOT that anyone previously carried a person ruling
//     across without asking. **`DR-6` REFUSED to generalise and D-51 records
//     that refusal as CORRECT**; what broke was its FACTUAL PREMISE. **A
//     REFUSAL IS NOT A COMMISSION.**
//
// Criteria carried here:
//
//   - W7-31(a) — GATING, with TWO CONTROLS per `W5d-G-2` item 1: (i) the
//     route registered WITHOUT the wrapper is judged REACHED; (ii) the
//     `requireLogin`-WRAPPED handler is judged NOT reached. ***A one-sided
//     control on a boolean judgement discriminates nothing.***
//   - W7-31(b) — the `w2RouteTable` CLASSIFICATION, with the two routes taking
//     DIFFERENT reasons. ⚠ This half is NOT paperwork: it is a PRODUCED RED
//     ON LOCKED W2 WORK.
//
// ⚠⚠ W7-31(c) — the `TP §4.8` SURFACE-TABLE ROW — IS **BOOKED, NOT ASSERTED**
// (`SEAM-W7-26`): `w2RouteTable`'s classes ARE `TP §4.8`'s two tables, so TWO
// REGISTRIES NEED A ROW AND ONLY ONE LIVES IN THE Go TREE, and a criteria file
// may not edit a design document. Owner: THE PLAN OWNER. This file asserts the
// Go-tree half only and does not pretend to the other.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL — it drives `srv.Handler()` and
// reads `w2RouteTable`, both of which exist — so it compiles and runs at
// e9cc5f5.
//
// EXPECTED RED at e9cc5f5 (both runtime):
//   - TestW7VehicleRoutesAreBehindRequireLogin: the route is not registered
//     at all, so an unauthenticated request REACHES the catch-all instead of
//     the login form.
//   - TestW7VehicleRoutesAreClassifiedInW2RouteTable: no row for
//     `/vehicle/amend` and no vehicle-creation row.
//     ⚠⚠ **THE PRE-CHUNK STATE IS ALREADY THE MUTANT**: `w2_route_walk_test.
//     go:233` emits "UNCLASSIFIED POST route %s registered at %s:%d — silence
//     is not exemption (TP §4.8): classify it … **at a criteria lock**" the
//     moment W7's POST routes are registered, and the failure message names
//     THIS LOCK as the only place the fix can come from.
//
// KILLER MUTANTS (§5):
//
//	(m7-31)  the route shipped UNGUARDED, or behind `dev_routes` instead of
//	         `requireLogin` — the `(m12)` shape. ⚠ [[D-12]] rules the second
//	         arm in terms and NAMES VEHICLE.       -> W7-31(a), TWO controls
//	(m7-31b) a route registered with NO classification row, reaching W2's
//	         walk unclassified — the `(m13)` shape -> W7-31(b)
package web

import (
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

// w7AmendPath is the vehicle amendment surface. `DR-W7-10` puts it IN W7's
// scope as a clone of `web/company_amend.go` and
// `templates/company_amend.html`, and `W7-31(b)` names the route by this
// spelling when it writes its `w2RouteTable` row.
const w7AmendPath = "/vehicle/amend"

// w7CreationReasonMarker is the DISTINCTIVE TEXT of the vehicle-creation
// route's classification reason, quoted from `W7-31(b)`'s own body.
//
// ⚠⚠ THE CREATION ROUTE IS **NOT** A FOURTH COPY OF THE AMENDMENT REASON, AND
// WRITING IT AS ONE WOULD BE A FALSE CLONE. It is reached FROM THE CHECK-IN
// FORM, so a typed value DOES travel with it — which is precisely why
// `W7-35` puts `vehicle_created` inside `W2-8`'s co-mint false-provenance
// guard. Its classification and `W7-35` MUST AGREE.
//
// ⚠ The route's PATH is deliberately not pinned here: the lock names the
// creation surface but does not spell its path, and inventing one would be a
// test-writer ruling a build choice. The row is found by its REASON, which
// the criterion does quote.
const w7CreationReasonMarker = "vehicle creation (W7, item 9)"

// w7AmendReasonMarker is the amendment route's reason, CLONED VERBATIM from
// the landed shape it is the FOURTH instance of [MEASURED —
// `w2_route_walk_test.go:158` `/person/amend` (W3), `:166` `/time/correct`
// (W4), `:174` `/company/amend` (W5d)].
const w7AmendReasonMarker = "amendment routes (W7, item 9): EXEMPT-with-written-reason — no typed value feeds the envelope"

// --- the fixture ---------------------------------------------------------

func w7WebServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w7web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 3600,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: "site-a",
	}
	return New(cfg, st), st
}

// w7AnonRequest issues an UNAUTHENTICATED request — the shared judgement
// behind W7-31(a)'s two controls and its real assertion. W7 mints its own
// rather than calling W5d's: W7-X3 forbids another chunk's green counting
// toward W7's.
func w7AnonRequest(t *testing.T, h http.Handler, method, path string, form url.Values) string {
	t.Helper()
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

// w7ReachedHandler is W7-31(a)'s JUDGEMENT FUNCTION: did an unauthenticated
// request REACH the handler?
//
// ⚠ Asserted on BODY CONTENT — the login form is served IN PLACE OF the
// handler — and NEVER on the status code: the catch-all answers 200 to
// everything, which is the house's own carry-forward.
func w7ReachedHandler(body string) bool {
	return !strings.Contains(body, `action="/login"`)
}

// =========================================================================
// W7-31(a) — GATING
// =========================================================================
func TestW7VehicleRoutesAreBehindRequireLogin(t *testing.T) {
	srv, st := w7WebServer(t)

	// --- CONTROL (i): the route registered WITHOUT the wrapper must be seen
	// to be REACHED by the same judgement the real assertion uses.
	unguarded := http.NewServeMux()
	unguarded.HandleFunc("GET "+w7AmendPath, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED unguarded vehicle amend page</h1>"))
	})
	unguarded.HandleFunc("POST "+w7AmendPath, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED unguarded vehicle amend handler</h1>"))
	})
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if !w7ReachedHandler(w7AnonRequest(t, unguarded, m, w7AmendPath, url.Values{})) {
			t.Fatalf("W7-31(a) CONTROL (i) FAILED (m7-31): an UNWRAPPED %s %s was judged NOT reached — the "+
				"gating judgement cannot detect an unguarded route, so its clean verdict proves nothing",
				m, w7AmendPath)
		}
	}

	// --- CONTROL (ii), THE ONE W7 MUST NOT INHERIT ONLY HALF OF: the
	// `requireLogin`-WRAPPED handler must be judged NOT reached. ***A
	// one-sided control on a boolean judgement discriminates nothing*** —
	// which is exactly why W5d needed a second one.
	guarded := http.NewServeMux()
	guarded.HandleFunc("POST "+w7AmendPath, srv.requireLogin(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<h1>PLANTED but guarded</h1>"))
	}))
	if w7ReachedHandler(w7AnonRequest(t, guarded, http.MethodPost, w7AmendPath, url.Values{})) {
		t.Fatal("W7-31(a) CONTROL (ii) FAILED: a requireLogin-WRAPPED handler was judged as REACHED — the " +
			"judgement discriminates nothing and its clean verdict on the real route would be meaningless")
	}

	// --- THE ASSERTION: the real routes, UNAUTHENTICATED -----------------
	for _, probe := range []struct {
		method string
		form   url.Values
	}{
		{http.MethodGet, nil},
		{http.MethodPost, url.Values{"vehicle_id": {"v-1"}, "number": {"Sneaked In"}}},
	} {
		body := w7AnonRequest(t, srv.Handler(), probe.method, w7AmendPath, probe.form)
		if w7ReachedHandler(body) {
			t.Errorf("W7-31(a) (m7-31): an UNAUTHENTICATED %s %s REACHED the handler.\n"+
				"⚠ [[D-63]] rules the officer may join two truck records FROM THE NORMAL SCREENS — that is WHO "+
				"may use it — and [[D-12]] rules the fix must NOT sit behind `dev_routes`. The `requireLogin` "+
				"MECHANISM is `DR-W7-17`, this lock's own design ruling, on parity of built shape with "+
				"/person/amend and /company/amend. ⚠ It cites NEITHER [[D-51]] NOR [[D-46]]: neither names "+
				"vehicle.\nBody: %s", probe.method, w7AmendPath, body)
		}
	}

	// --- AND AUTHENTICATED, the route must be REACHABLE ------------------
	//
	// ⚠ Without this arm a route that does not exist at all satisfies the
	// gating assertion perfectly: "not reached" is exactly what a 404 looks
	// like. [[D-63]] is not satisfied by a surface nobody can reach, and
	// `W7-15(d)` makes the same point one criterion over — "a surface that is
	// registered, gated and impossible to operate delivers the APPEARANCE of
	// D-63, not D-63."
	cookie := loginCookie(t, srv, testLoginPassword)
	req := httptest.NewRequest(http.MethodGet, w7AmendPath, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `action="`+w7AmendPath+`"`) {
		t.Errorf("W7-31(a): an AUTHENTICATED GET %s renders no form posting back to it (status %d). "+
			"⚠ The gating assertion alone is satisfied by a route THAT DOES NOT EXIST — \"not reached\" is what "+
			"a 404 looks like — so the reachable arm is what stops this criterion passing on an absent surface.\n"+
			"Body: %s", w7AmendPath, rec.Code, rec.Body.String())
	}
	_ = st
}

// =========================================================================
// W7-31(b) — the `w2RouteTable` CLASSIFICATION, in the same commit
// =========================================================================
//
// ⚠ THE §4-SHAPED GRANT, carried so the builder's append is sanctioned IN
// ADVANCE: post-lock §4 sanctions an append to `w2_route_walk_test.go` "with
// the authority for the row coming from the criterion" — AND WITH NO
// CRITERION THE APPEND IS AN UNSANCTIONED TEST EDIT. `W7-31(b)` is that
// authority; this test is its check.
//
// ⚠ THIS TEST DOES NOT MAKE THE APPEND. It asserts that it was made. The
// append is the BUILDER's, in the same commit that registers the route
// ([[D-51]]'s binding consequence 3, which [[D-63]] cites at
// `JAMES-DECISIONS.md:3374-3377`).
func TestW7VehicleRoutesAreClassifiedInW2RouteTable(t *testing.T) {
	// --- CONTROL FIRST: the row finder must discriminate. A finder that
	// reports "found" for everything, or "not found" for everything, makes
	// the assertion vacuous in one direction or permanently red in the other.
	probe := []w2Row{
		{Route: "/person/amend", Class: w2Exempt, Reason: "amendment routes (W3): EXEMPT-with-written-reason — no typed value feeds the envelope"},
	}
	if _, ok := w7FindRoute(probe, "/person/amend"); !ok {
		t.Fatal("W7-31(b) CONTROL FAILED: the row finder did not find a row that is present")
	}
	if _, ok := w7FindRoute(probe, w7AmendPath); ok {
		t.Fatal("W7-31(b) CONTROL FAILED (negative arm): the row finder reported a row that is absent")
	}
	if w7AnyReasonContains(probe, w7CreationReasonMarker) {
		t.Fatal("W7-31(b) CONTROL FAILED (negative arm): the reason finder matched text that is not there")
	}
	if !w7AnyReasonContains(probe, "amendment routes (W3)") {
		t.Fatal("W7-31(b) CONTROL FAILED: the reason finder missed text that is there")
	}

	// --- THE ASSERTION ---------------------------------------------------
	//
	// ⚠ THE PRE-CHUNK STATE IS ALREADY THE MUTANT: `w2_route_walk_test.go:233`
	// emits "UNCLASSIFIED POST route … classify it takes-input,
	// exempt-with-written-reason, or authors-nothing-with-proof AT A CRITERIA
	// LOCK" the moment W7's POST routes are registered, and the walk globs
	// `mux.HandleFunc("POST …")` across `internal/web/*.go` — so W7's
	// registrations are DISCOVERED WHETHER OR NOT ANY TEST NAMES THEM.
	row, ok := w7FindRoute(w2RouteTable, w7AmendPath)
	if !ok {
		t.Errorf("W7-31(b) (m7-31b): `w2RouteTable` carries NO ROW for %s. It is the FOURTH instance of a shape "+
			"that already has three — /person/amend (W3), /time/correct (W4), /company/amend (W5d) — and the row "+
			"is CLONED VERBATIM:\n  %q\n"+
			"⚠ `w2RouteTable` changes ONLY at a chunk's criteria lock, each new row naming its reason there; "+
			"builders may not edit tests, so no exemption can grow by a build's say-so. This criterion is the "+
			"authority for the append.", w7AmendPath, w7AmendReasonMarker)
	} else {
		if row.Class != w2Exempt {
			t.Errorf("W7-31(b): %s is classified %v, want EXEMPT-with-written-reason", w7AmendPath, row.Class)
		}
		if !strings.Contains(row.Reason, w7AmendReasonMarker) {
			t.Errorf("W7-31(b): %s's written reason is %q — it must be the landed amendment-route reason CLONED "+
				"VERBATIM:\n  %q", w7AmendPath, row.Reason, w7AmendReasonMarker)
		}
	}

	// ⚠⚠ THE VEHICLE CREATION ROUTE TAKES A DIFFERENT REASON, and writing it
	// as a fourth copy of the amendment one would be A FALSE CLONE: it is
	// reached FROM THE CHECK-IN FORM, so a typed value DOES travel with it.
	// `W7-35` settles it — THE CO-MINT TAKES DEFAULT CAPTURE — so the route's
	// written reason must SAY SO rather than borrow a sentence about payload,
	// and the two must AGREE.
	if !w7AnyReasonContains(w2RouteTable, w7CreationReasonMarker) {
		t.Errorf("W7-31(b) (m7-31b): `w2RouteTable` carries no row whose written reason names %q.\n"+
			"⚠⚠ The creation route is NOT a fourth copy of the amendment reason. It is reached from the check-in "+
			"form, so a typed value DOES travel with it — which is precisely why `W7-35` puts `vehicle_created` "+
			"inside `W2-8`'s co-mint false-provenance guard. ITS CLASSIFICATION AND `W7-35` MUST AGREE: if the "+
			"co-mint takes default/now under `W2-8`, the route's written reason must say so.\n"+
			"⚠ The row's PATH is deliberately not pinned by this test — the lock names the creation surface and "+
			"does not spell its path, and inventing one would be a test-writer ruling a build choice.",
			w7CreationReasonMarker)
	}
}

func w7FindRoute(table []w2Row, route string) (w2Row, bool) {
	for _, r := range table {
		if r.Route == route {
			return r, true
		}
	}
	return w2Row{}, false
}

func w7AnyReasonContains(table []w2Row, marker string) bool {
	for _, r := range table {
		if strings.Contains(r.Reason, marker) {
			return true
		}
	}
	return false
}
