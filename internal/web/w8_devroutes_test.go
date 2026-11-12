// w8_devroutes_test.go — W8-9(a-4): the ROUTE-ABSENCE limb.
//
// ⟦LOCK — F-1 / XCH-F12, an UPHELD MAJOR that reached NEITHER Pass B's
// dispositions NOR revision 3 of the draft — "not merged, deferred or
// declined anywhere". Applied in the arbiter's own terms.⟧
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠ WHAT (a-1)…(a-3) DO NOT GIVE: (a-2) and (a-3) establish that nothing
// AUTHORS the event and that the walk classifies what is registered; NEITHER
// ASSERTS THE ROUTE IS GONE FROM A RUNNING MUX. (m8-22) gives the
// gated-not-removed shape as a mutation, and nothing gave the re-registered
// one.
//
// The model is internal/web/web_test.go's TestNoProjectionDumpRouteExists,
// cited because it is LANDED, not invented here.
package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// TestW8DevAliasRouteAuthorsNothing is W8-9(a-4).
//
// WHAT THIS LIMB ASSERTS, in terms: with the server built from a config
// carrying DevRoutes: true, a POST /dev/alias request whose body the
// pre-chunk build WOULD HAVE ACCEPTED appends NO ENVELOPE to the log and
// leaves NO ref_alias row and no resolvable alias edge in the store.
//
// ⚠⚠ `DevRoutes: true` IS LOAD-BEARING, AND THIS IS THE WHOLE FINDING.
// [MEASURED — internal/web/devroutes.go:29-32] registerDevRoutes returns
// early when !s.Cfg.DevRoutes, so a DevRoutes:false fixture proves absence
// ONLY UNDER A CONFIGURATION THIS PROGRAM NEVER RUNS. dev_routes = true in
// all four shipped deploy TOMLs. ⚠ A DevRoutes:false fixture here reproduces
// D-56's ERRATUM exactly, one route over.
//
// ⚠⚠ NEVER A STATUS CODE, and BOTH wrong readings are named so a test-writer
// cannot pick one by accident:
//
//	404 is the trap — a test-writer told "prove /dev/alias is gone" will
//	    assert it and GET A GREEN FROM THE GO 1.22 CATCH-ALL;
//	405 is the subtler trap — a POST to a path with no POST pattern answers
//	    405 Method Not Allowed, which is green WHETHER OR NOT the route was
//	    re-registered under another method or another spelling.
//
// THE ASSERTION IS ON WHAT THE REQUEST DID, NEVER ON WHAT IT ANSWERED.
//
// ⭐ GENUINE PRE-CHUNK RED: devroutes.go:32 registers the route, alias.go
// mints the envelope, and dev_routes = true in every shipped TOML — so a
// POST really does mint an alias today.
func TestW8DevAliasRouteAuthorsNothing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}

	// PRECONDITION, failing loud in w5d_scoped_read_test.go:216-228's landed
	// idiom, because WITHOUT IT THE ABSENCE IS VACUOUS: the two ids in the
	// request body must be ids the pre-chunk build WOULD have aliased. A
	// malformed body is refused by every build, INCLUDING A BUILD THAT STILL
	// SERVES THE ROUTE. ⚠ `PRECONDITION:` is never a production red — the
	// model's own analogue is its "the store cannot dump at all" setup check.
	dup, err := domain.CreateCompany(st, "site-a", 1, "Duplicate Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	canon, err := domain.CreateCompany(st, "site-a", 1, "Canonical Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if dup.CompanyID == "" || canon.CompanyID == "" || dup.CompanyID == canon.CompanyID {
		t.Fatalf("PRECONDITION: the fixture's two ids are not two distinct real rows (%q, %q) — a body the "+
			"pre-chunk build would have REFUSED proves nothing about the route being gone", dup.CompanyID, canon.CompanyID)
	}

	// ⚠ DevRoutes: true. See the comment above; this is the setting the test
	// would be worthless without.
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		DevRoutes: true, LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st)

	before := w8EventCount(t, st, "site-a", 1)

	// The route is behind requireLogin, so the request is made AS A LOGGED-IN
	// OFFICER — otherwise the absence measured is the login gate's, not the
	// route's removal.
	rec := postForm(t, srv, "/dev/alias", url.Values{
		"duplicate_id": {dup.CompanyID},
		"canonical_id": {canon.CompanyID},
	})
	_ = rec // ⚠ DELIBERATELY UNASSERTED: never a status code. See above.

	after := w8EventCount(t, st, "site-a", 1)
	if after != before {
		t.Errorf("W8-9(a-4) ((m8-22)): POST /dev/alias appended %d envelope(s) to the log (%d -> %d) with "+
			"DevRoutes: true. The route is LIVE and it mints.\n"+
			"⚠ `POST /dev/alias` is REMOVED, not gated: gating it behind cfg.DevRoutes is a NO-OP because "+
			"dev_routes = true in ALL FOUR shipped deploy TOMLs — D-56's ERRATUM as a general fact about this "+
			"program — and \"only removal closes it\".\n"+
			"⚠ THE ASSERTION IS ON WHAT THE REQUEST DID, NEVER ON WHAT IT ANSWERED: 404 is a green from the Go "+
			"1.22 catch-all and 405 is green whether or not the route came back under another method or spelling",
			after-before, before, after)
	}

	// --- ARM (B), RE-EXPRESSED ⟦LOCK — §55, 2026-09-03⟧ -------------------
	//
	// ⟦was: "if aliases, err := domain.RefAliases(st); err == nil && len(aliases) != 0 {"⟧
	//
	// ⚠⚠ THE SUPERSEDED FORM COULD NOT FAIL, WHICH IS WHY §55 RULED.
	// `domain.RefAliases` was a shim over a deleted mechanism returning
	// (nil, non-nil error), so `err == nil` was ALWAYS FALSE and the guard
	// never reached its own subject. Allowlisting it — the disposition the
	// shim's own header proposed — would have frozen a check that CANNOT FAIL
	// as cover for the exact regression [[D-47]] exists to prevent.
	//
	// ⚠ THE RE-EXPRESSION NAMES THE WIRE SPELLING, NEVER A GO SYMBOL. That is
	// the point: the mechanism's Go surface is gone, so an assertion naming it
	// cannot be written — and one naming a shim can be answered by the shim.
	//
	// ⚠ WHAT IS DELIBERATELY *NOT* ASSERTED: "no `ref_alias` section in the
	// projection dump". Nothing registers such a section, so that arm would be
	// TRUE BY CONSTRUCTION and could not be planted against — it would re-make
	// the very defect this ruling removes.

	// CONTROL FIRST, AND PRODUCED: the predicate must see its own plant, or
	// the clean pass below says nothing.
	ctl, err := store.Open(filepath.Join(t.TempDir(), "arm-b-control.db"))
	if err != nil {
		t.Fatalf("ARM (B) CONTROL SETUP FAILED: %v", err)
	}
	defer ctl.Close()
	if err := domain.Register(ctl); err != nil {
		t.Fatalf("ARM (B) CONTROL SETUP FAILED: %v", err)
	}
	planted := json.RawMessage(`{"type":"` + w8DeletedAliasEventType + `","duplicate_id":"dup-1","canonical_id":"canon-1"}`)
	// Apply, not Author: this plant simulates a row from BEFORE the
	// mechanism was retired, already sitting in the log (replicated or
	// replayed), never a LIVE authoring of a retired type today. G13 fails
	// Author closed on exactly this wire type — it is absent from domain's
	// classified set on purpose, per [[D-47]] — but Apply never consults
	// the ownership registry at all (W3-2), which is precisely why a
	// legacy/peer row plants here without resurrecting the deleted
	// mechanism as something this post could author afresh.
	if _, err := ctl.Apply(store.Envelope{
		SiteID: "site-a", NodeEpoch: 1, Seq: 1, SchemaVersion: store.SchemaVersion, OriginAuth: "site-a",
		Payload: planted,
	}); err != nil {
		t.Fatalf("ARM (B) CONTROL SETUP FAILED: a %s envelope could not be planted, so the control cannot "+
			"establish that this arm can fail: %v", w8DeletedAliasEventType, err)
	}
	if n := w8DeletedAliasEventsIn(t, ctl, "site-a", 1); n != 1 {
		t.Fatalf("W8-9(a-4) ARM (B) CONTROL FAILED: the predicate found %d planted %q envelope(s), want 1. "+
			"A check that cannot see its own plant proves nothing — which is precisely how the SUPERSEDED form "+
			"of this arm stayed green while the mechanism it guards was unguarded", n, w8DeletedAliasEventType)
	}

	// --- THE ASSERTION ---------------------------------------------------
	if n := w8DeletedAliasEventsIn(t, st, "site-a", 1); n != 0 {
		t.Errorf("W8-9(a-4) ARM (B): %d %q envelope(s) were authored into the stream by the request. The "+
			"mechanism is DELETED by P-3 §5 and [[D-47]] sets the bar: \"ref_alias_created must NOT be carried "+
			"into v10's frozen event-type vocabulary… A freeze list that carries it forward re-creates the "+
			"retired mechanism as a frozen ghost.\"\n"+
			"⚠ This arm survives a route re-registered under a DIFFERENT PATH SPELLING, which is what no "+
			"existing mutant produced and what a status-code assertion cannot see.",
			n, w8DeletedAliasEventType)
	}
}

// w8DeletedAliasEventType is the WIRE spelling of the retired alias event —
// a string literal, never a Go symbol. ⟦LOCK — §55, 2026-09-03⟧
const w8DeletedAliasEventType = "ref_alias_created"

// w8DeletedAliasEventsIn counts envelopes in the (site, epoch) stream whose
// payload `type` is the retired alias event. ⟦LOCK — §55, 2026-09-03⟧
func w8DeletedAliasEventsIn(t *testing.T, st *store.Store, site string, epoch int64) int {
	t.Helper()
	evs, err := st.Events(site, epoch, 1, 0)
	if err != nil {
		t.Fatalf("PRECONDITION (instrument): the log is unreadable, so \"authored nothing\" cannot be measured: %v", err)
	}
	n := 0
	for _, e := range evs {
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(e.Payload, &p) == nil && p.Type == w8DeletedAliasEventType {
			n++
		}
	}
	return n
}

// w8EventCount reads the (site, epoch) stream length. Counting the LOG is
// what makes this an assertion about what the request DID.
func w8EventCount(t *testing.T, st *store.Store, site string, epoch int64) int {
	t.Helper()
	evs, err := st.Events(site, epoch, 1, 0)
	if err != nil {
		t.Fatalf("PRECONDITION (instrument): the log is unreadable, so \"appended nothing\" cannot be measured: %v", err)
	}
	return len(evs)
}

func w8WebModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := osGetwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

var _ = httptest.NewRequest

// --- small local helpers, kept here so this file adds no dependency on
//     another window's test harness -------------------------------------

func osGetwd() (string, error) { return os.Getwd() }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func readFileTrimmed(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
