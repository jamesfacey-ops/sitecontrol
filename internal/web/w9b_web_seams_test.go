// w9b_web_seams_test.go — W9b (item 16) shared machinery for the web-layer
// criteria. NO ASSERTIONS LIVE HERE.
//
// The design decision every W9b file rests on is stated in full in
// internal/domain/w9b_seams_test.go: no W9b test names an unbuilt Go symbol,
// so retractions are authored as HAND-BUILT ENVELOPES carrying F4 §3.6's
// frozen wire JSON and applied through the real store.Apply, and every
// assertion is made at a surface or reader that exists TODAY. Every W9b web
// test therefore COMPILES AND RUNS against the pre-chunk tree and reds at
// RUNTIME naming its criterion.
//
// ⚠ WHICH TESTS AUTHOR OVER THE WIRE AND WHICH POST A FORM, and why the split
// matters. W9b-1 is about the FOLD's consequences — reopen, board row, alert,
// muster, peer. Its fixtures author the retraction as an envelope, so a
// W9b-1 red is never really "the route is missing". W9b-2 and W9b-5 are about
// the SURFACE, so they go through `POST /arrivals/unref` and red at the route
// exactly as they should.
//
// --- SEAMS BOUND HERE, ONCE ------------------------------------------------
//
// SEAM W9B-S-FORM — the un-ref form's fields. F4 §3.6 declares them: "the
// un-ref form (`GET`/`POST /arrivals/unref` — fields: `arrival_id`, nothing
// else)". This is a quotation, not a judgement.
//
// SEAM W9B-S-CONFIRM — the confirmation is a GET-able page. W9b-2 requires
// success to be "a POST-RECOMPUTE READ, never a pre-append prediction —
// follow the 303 and read the rendering off X's recomputed `trip_state`", and
// F4 §3.6 says the strings are "functions of rows that exist AFTER the append
// and its recompute", correct "at render time". The crossing case (T12c's
// second half) can only be checked if the 303's target can be fetched AGAIN
// after the peer's arrival lands — so this suite treats the confirmation as a
// GET route whose content is derived per render. A builder who makes it a
// one-shot flash message instead has made the crossing case uncheckable and
// takes that to the GATE.
//
// SEAM W9B-S-COPY — which literal tokens each of the three renderings must
// carry. F4 §3.6 writes the three strings out; this suite asserts the tokens
// the CRITERIA name, not whole sentences: the state word (`OPEN` /
// `ARRIVED`), the binder's identity (an arrival id and a post name, both
// fixture literals), the automatic-match disclosure (`automatic` and the
// no-officer-selection phrase), and `SUPPRESSED` for the standing-ack
// rendering. ⚠ It deliberately asserts NOTHING about wording STYLE, and
// nothing that compares W9b's confirmation with W9a's or W9c's: [[D-62]]
// routes "whether W9's three confirmation screens use one wording style or
// three" to W9's BUILD CRITIQUE, and a test here would pre-empt a question
// James's ruling placed somewhere else.
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- routes and fields ----------------------------------------------------

const (
	w9bUnrefPath    = "/arrivals/unref"
	w9bTripsPath    = "/trips"
	w9bMusterLive   = "/muster"
	w9bMusterPrint  = "/muster/print"
	w9bFieldArrival = "arrival_id"
)

// --- frozen wire spellings (F4 §3.6) --------------------------------------

const (
	w9bEvtRefRetracted = "arrival_ref_retracted"
	w9bKeyType         = "type"
	w9bKeyArrivalID    = "arrival_id"
	w9bKeyRecordedBy   = "recorded_by"
)

// w9bPayloadKeys is the payload's key set, sorted — W9b-2's
// filter-independent structural literal.
var w9bPayloadKeys = []string{w9bKeyArrivalID, w9bKeyRecordedBy, w9bKeyType}

const w9bMinute = int64(60_000)

func w9bStr(s string) *string { return &s }
func w9bI64(v int64) *int64   { return &v }

// --- the wire path --------------------------------------------------------

func w9bEnvelope(t *testing.T, siteID string, epoch, seq, wallMS int64, payload any) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		AuthoredAt: store.AuthoredAt{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced},
		OccurredAt: wallMS, TimeSource: "default", Payload: raw,
	}
}

func w9bRetraction(arrivalID string, recordedBy *string) map[string]any {
	m := map[string]any{w9bKeyType: w9bEvtRefRetracted, w9bKeyArrivalID: arrivalID}
	if recordedBy != nil {
		m[w9bKeyRecordedBy] = *recordedBy
	}
	return m
}

// w9bApplyNext applies a hand-built envelope at the stream's next free seq
// and returns it, so a caller can replicate the same bytes to a peer.
func w9bApplyNext(t *testing.T, st *store.Store, siteID string, epoch, wallMS int64, payload any) store.Envelope {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	env := w9bEnvelope(t, siteID, epoch, wm+1, wallMS, payload)
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9b: apply %v: %v — folds never refuse (F4 §3.6: the fold is a bare INSERT, dangling-tolerant)", env.Key(), err)
	}
	if !applied {
		t.Fatalf("w9b: apply %v: deduped unexpectedly", env.Key())
	}
	return env
}

func w9bSeedPerson(t *testing.T, st *store.Store, siteID string, epoch int64, personID, name string) {
	t.Helper()
	w9bApplyNext(t, st, siteID, epoch, w2Base, map[string]any{
		"type": "person_created", "person_id": personID, "name": name,
	})
}

// w9bReplicate copies one site's whole stream from src into dst, exactly as
// the replication engine delivers it: the contiguous prefix, in order (the
// store refuses a gap, §8.3/§8.4, and cherry-picking one envelope would be
// testing a delivery this system never performs).
func w9bReplicate(t *testing.T, src, dst *store.Store, siteID string, epoch int64) {
	t.Helper()
	evs, err := src.Events(siteID, epoch, 1, 0)
	if err != nil {
		t.Fatalf("w9b: reading stream (%s, %d): %v", siteID, epoch, err)
	}
	for _, e := range evs {
		if _, err := dst.Apply(e); err != nil {
			t.Fatalf("w9b: replicating %v: %v", e.Key(), err)
		}
	}
}

// --- the two posts --------------------------------------------------------

// w9bPosts is one two-post world: the ORIGIN (site-a, "Ice Road") and the
// DESTINATION (site-b, "Pad Site Control"), each with its own store, its own
// server and its own live deadline scanner. Both are needed because W9b-1's
// peer half is a POSITIVE cross-post assertion — "no peer assertion in W9b
// may be a bare absence".
type w9bPosts struct {
	Origin      *Server
	OriginStore *store.Store
	OriginScan  *deadline.Deadlines

	Dest      *Server
	DestStore *store.Store
	DestScan  *deadline.Deadlines
}

func w9bNewPosts(t *testing.T, name string) *w9bPosts {
	t.Helper()
	p := &w9bPosts{}
	p.OriginStore = w3OpenRegistered(t, name+"-origin.db")
	p.Origin = New(w3ConfigSiteA(p.OriginStore.Path), p.OriginStore)
	p.OriginScan = w9bWireScanner(t, p.Origin, p.OriginStore, "site-a")

	p.DestStore = w3OpenRegistered(t, name+"-dest.db")
	p.Dest = New(w3ConfigSiteB(p.DestStore.Path), p.DestStore)
	p.DestScan = w9bWireScanner(t, p.Dest, p.DestStore, "site-b")
	return p
}

func w9bWireScanner(t *testing.T, srv *Server, st *store.Store, siteID string) *deadline.Deadlines {
	t.Helper()
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, siteID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	srv.Alerts = d
	return d
}

// --- fixture worlds -------------------------------------------------------

// w9bDispatchOpen dispatches a controlled trip from origin to destination.
func w9bDispatchOpen(t *testing.T, st *store.Store, origin, destination string, epoch int64, vehicle, driver string, occupants []string, deadlineMinutes int64) domain.Trip {
	t.Helper()
	dest := destination
	trip, err := domain.Dispatch(st, origin, epoch, domain.DispatchInput{
		// ⟦LOCK — §50 arrival re-seed, 2026-09-03⟧ UN-ROUTED. The §47 route below is DISCHARGED and is
		// PRESERVED VERBATIM UNDERNEATH rather than deleted — it is HISTORY, not a live routing.
		// §50 rules that the ARRIVAL half receives the same transformation via the same helper;
		// this pass applied it at this fixture's `RecordUnsolicitedArrival` call, so the arrival now
		// carries a vehicle RECORD id, `vehicleNumberSnapshot` populates `trip_arrival.vehicle_entered`
		// again, and the correlation this fixture asserts is TRUE for the reason it always meant.
		// ⟦was ROUTED — §47 condition 3, 2026-09-03 — DISCHARGED by §50⟧ NOT RE-SEEDED. Re-seeding the TRIP half
		// makes this fixture's arrival assertion FALSE, not merely uncompilable.
		// `W7-4(d)(i)` re-typed `RecordUnsolicitedArrival`'s FOURTH POSITIONAL PARAMETER from
		// the number-as-typed to a vehicle RECORD id WITHOUT changing arity or type
		// (tripauthor.go, its own ⟦was: "…, vehicleEntered, originSite string, …"⟧), so the
		// unsolicited call in this fixture still COMPILES while passing a number.
		// ⚠ MEASURED on production code alone, no test file involved:
		//     number as the 4th arg -> vehicle_id="TRUCK 12"  snapshot=<nil>  disposition=PLAIN
		//     record id as the 4th arg -> vehicle_id="site-b-vehicle-…"  snapshot="TRUCK 12"  CORRELATED
		// `vehicleNumberSnapshot` finds no record for the number, so the arrival's snapshot is
		// NIL and it contributes no matching key (trip.go's own comment does the rest).
		// Repairing the arrival half is ONE root cause across 27 call sites and is the PLAN
		// OWNER's. See reports/window-build/RESEED-164.md §3.7.
		// ⚠ THIS IS A SHARED FIXTURE HELPER. Its callers — listed in RESEED-164.md §4.5 —
		// include files with no `VehicleEntered` site of their own, so a re-seed here reddens
		// tests whose own assertion diff is EMPTY.
		// ⟦was: "VehicleEntered: vehicle, DriverPersonID: driver, OccupantPersonIDs: occupants,"⟧
		VehicleID: w7SeedVehicleID(t, st, origin, vehicle), DriverPersonID: driver, OccupantPersonIDs: occupants,
		DestinationSite: &dest, DeadlineMinutes: w9bI64(deadlineMinutes),
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9b fixture: dispatch: %v", err)
	}
	return trip
}

// w9bCaptureLiveOverdue gives the scanner the receipt anchor that makes a
// trip's countdown expire LIVE rather than landing in the partition-expired
// review case — `alerts_test.go`'s own `dispatchLiveOverdueTrip` shape, which
// is this house's way of producing an `overdue_live` row.
func w9bCaptureLiveOverdue(t *testing.T, d *deadline.Deadlines, st *store.Store, siteID string, epoch, seq int64) {
	t.Helper()
	envs, err := st.Events(siteID, epoch, seq, 1)
	if err != nil || len(envs) != 1 {
		t.Fatalf("w9b fixture: fetching the dispatch envelope at (%s,%d,%d): %v", siteID, epoch, seq, err)
	}
	d.OnApplied(envs[0], time.Now().Add(-time.Minute+80*time.Millisecond), true)
	time.Sleep(150 * time.Millisecond)
}

// --- observing the board --------------------------------------------------

// w9bBannerFor returns the banner row for tripID, or nil. The banner is what
// every session must surface; "the alert is BACK" and "no alert at either
// post" are both stated over it.
func w9bBannerFor(t *testing.T, d *deadline.Deadlines, tripID string) *deadline.Alert {
	t.Helper()
	board, err := d.Evaluate()
	if err != nil {
		t.Fatalf("w9b: deadline evaluate: %v", err)
	}
	for _, a := range board.Banner() {
		if a.TripID == tripID {
			row := a
			return &row
		}
	}
	return nil
}

func w9bBannerSummary(t *testing.T, d *deadline.Deadlines) string {
	t.Helper()
	board, err := d.Evaluate()
	if err != nil {
		return "evaluate failed: " + err.Error()
	}
	rows := board.Banner()
	if len(rows) == 0 {
		return "empty"
	}
	var out []string
	for _, a := range rows {
		out = append(out, a.TripID+"="+string(a.Status))
	}
	return strings.Join(out, ", ")
}

// --- rendered pages -------------------------------------------------------

func w9bGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := w2Get(t, srv, path, loginCookie(t, srv, testLoginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("w9b: GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// w9bSheetStampRe matches the muster's render stamp — `musterSheetStamp`'s
// "2006-01-02 15:04 MST" format. It is normalised out of a byte-identity
// comparison because it is the RENDER time and changes with the clock, not
// with the data: a byte comparison defeated by a minute rolling over is a
// fixture defect, not a finding. Nothing else is normalised, and the
// normalisation is asserted to have actually matched something (below), so it
// cannot silently widen into deleting the page.
var w9bSheetStampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} [A-Z]{2,5}`)

// w9bMusterBytes fetches a muster page and normalises ONLY the render stamp.
func w9bMusterBytes(t *testing.T, srv *Server, path string) string {
	t.Helper()
	page := w9bGet(t, srv, path)
	if !w9bSheetStampRe.MatchString(page) {
		t.Fatalf("w9b FIXTURE DEFECT: no render stamp found on %s, so the normalisation below is not doing what its comment says. "+
			"If `musterSheetStamp`'s format changed, this regex must change with it — an over-wide normalisation would delete the very bytes the comparison rests on", path)
	}
	return w9bSheetStampRe.ReplaceAllString(page, "<RENDER-STAMP>")
}

// w9bReviewRowRe extracts the review-lane rows of the /trips page. The
// template renders each as `<div class="notice notice-warn"
// data-review-kind="…">…</div>` — an in-template structural handle that
// already exists, so the assertion is SCOPED TO THE ROW rather than to the
// whole page. That matters: a post's display name appears in the topbar and
// in the destination list, so "the retracting post appears on the page" would
// be true of a page with no review row at all.
var w9bReviewRowRe = regexp.MustCompile(`(?s)<div[^>]*data-review-kind="([^"]*)"[^>]*>(.*?)</div>`)

func w9bReviewRows(page string) []string {
	var out []string
	for _, m := range w9bReviewRowRe.FindAllStringSubmatch(w2StripNonMarkup(page), -1) {
		out = append(out, m[1]+": "+m[2])
	}
	return out
}

// --- form controls --------------------------------------------------------

// w9bControlNameRe pulls the `name="…"` of every form control on a page.
var w9bControlNameRe = regexp.MustCompile(`<(?:input|select|textarea|button)\b[^>]*\bname="([^"]*)"`)

func w9bControlNames(page string) []string {
	var out []string
	for _, m := range w9bControlNameRe.FindAllStringSubmatch(w2StripNonMarkup(page), -1) {
		out = append(out, m[1])
	}
	return out
}

// --- posting to the un-ref surface ----------------------------------------

// w9bUnref posts one un-reference and returns the redirect Location (empty if
// the response was not a redirect), the page the officer reads next, the
// status code, and whether the stream's watermark moved — "nothing authored"
// is asserted on the LOG, never on a status code.
func w9bUnref(t *testing.T, srv *Server, st *store.Store, form url.Values) (loc, page string, code int, authored bool) {
	t.Helper()
	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec, cookie := w2PostForm(t, srv, w9bUnrefPath, form)
	code = rec.Code
	loc = rec.Result().Header.Get("Location")
	if code == http.StatusSeeOther || code == http.StatusFound {
		page = w9bGet(t, srv, loc)
	} else {
		page = rec.Body.String()
	}
	_ = cookie
	after := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	return loc, page, code, after != before
}

// w9bRouteExists reports whether the mux has a real handler for path — so a
// missing route reds with a sentence naming the criterion instead of with an
// unexplained 200 from the catch-all status page.
func w9bRouteExists(t *testing.T, srv *Server, method, path string) bool {
	t.Helper()
	req, err := http.NewRequest(method, "http://example.invalid"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, pattern := srv.Handler().(*http.ServeMux).Handler(req)
	return pattern != "" && pattern != "GET /" && !strings.HasPrefix(pattern, "GET / ")
}

// w9bConfigPostName is the derivation VF §5 requires: the retracting post's
// rendered identity comes from the config the server loaded, never from a
// hardcoded name in a test.
func w9bConfigPostName(cfg *config.Config) string { return cfg.PostName() }
