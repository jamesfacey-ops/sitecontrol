// C10 done-when tests, HTTP/template rendering layer. These drive the real
// srv.Handler() and the real embedded templates end to end — belt-and-
// suspenders alongside syncstatus_test.go's pure view-model tests, catching
// anything a template typo could break that the Go-level tests can't see
// (e.g. a wrong field name silently rendering the zero value).
package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, p := range []store.Projection{
		{Name: "trip", Table: "trip", OrderBy: "trip_id"},
		{Name: "visit", Table: "visit", OrderBy: "visit_id"},
	} {
		if err := st.RegisterProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{SiteID: "site-b", DisplayName: "Site B", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10}
	return New(cfg, st)
}

func getStatusPage(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// --- not-done-if: SyncSnapshot unwired must render an explicit refusal,
// never a false "ok" and never silence ---

func TestStatusPageWithSyncSnapshotUnsetRendersUnavailableNotHealthy(t *testing.T) {
	srv := newTestServer(t) // SyncSnapshot left nil
	body := getStatusPage(t, srv)
	if !strings.Contains(body, "SYNC STATUS UNAVAILABLE") {
		t.Fatalf("body missing the unavailable refusal; got:\n%s", body)
	}
	if strings.Contains(body, `data-health="ok"`) {
		t.Fatal("an unavailable sync section rendered a peer box marked healthy")
	}
}

// --- done-when: healthy peer renders "last heard from <peer>: N sec/min ago" ---

func TestStatusPageRendersLastHeardFromPerPeer(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-b",
			Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(7)}},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 3, Head: 3, State: repl.StateOK},
			},
		}
	}
	body := getStatusPage(t, srv)
	if !strings.Contains(body, "last heard from site-a") {
		t.Fatalf("body missing the §8.9 'last heard from <peer>' line; got:\n%s", body)
	}
	if !strings.Contains(body, "7 seconds ago") {
		t.Fatalf("body missing the actual age; got:\n%s", body)
	}
}

// --- done-when: C3a's schema-mismatch case renders as a queued/refused
// state, not silence (M5) ---

func TestStatusPageRendersSchemaMismatchAsQueuedRefusalNotSilence(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-b",
			Peers:  []repl.PeerSnapshot{{SiteID: "site-x", LastContactAgoSec: f(2)}},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "site-x", NodeEpoch: 1}, Watermark: 0, Head: 9,
					State: repl.StateSchemaMismatch, Detail: "schema_version 6 != 5"},
			},
		}
	}
	body := getStatusPage(t, srv)
	if !strings.Contains(body, "SCHEMA MISMATCH") || !strings.Contains(body, "queued") {
		t.Fatalf("body missing the visible schema-mismatch refusal; got:\n%s", body)
	}
	// F19 (test-suite audit): the ORIGINAL form of this assert was
	// `!strings.Contains(body, "c10-bad")`, which is vacuous — ".c10-bad"
	// sat in the inline <style> block on EVERY render (defining the CSS
	// rule), so a raw substring search for it was always true regardless of
	// which peer box actually got the class. It was then anchored on the
	// class="..." attribute, which fixed that instance but left the whole
	// family one shared-stylesheet edit away from repeating it.
	// U1 (design foundation) closes the class off entirely: the peer box now
	// carries data-health="ok"/"degraded", an attribute that appears in NO
	// selector in the one shared stylesheet, so a stylesheet match can never
	// stand in for a rendered element here again. Renaming a CSS class can
	// no longer silently change what this test proves.
	if !strings.Contains(body, `data-health="degraded"`) {
		t.Fatalf("schema-mismatch peer did not render as degraded; got:\n%s", body)
	}
	if strings.Contains(body, `data-health="ok"`) {
		t.Fatal("schema-mismatch peer rendered as HEALTHY")
	}
}

// --- done-when: C3b's regression test case renders the regressed state ---

func TestStatusPageRendersPeerLogRegressedState(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-b",
			Peers:  []repl.PeerSnapshot{{SiteID: "site-x", LastContactAgoSec: f(1)}},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "site-x", NodeEpoch: 1}, Watermark: 10, Head: 3,
					State: repl.StateRegressed, Detail: "peer log regressed (§8.9)"},
			},
		}
	}
	body := getStatusPage(t, srv)
	if !strings.Contains(body, "PEER LOG REGRESSED") {
		t.Fatalf("body missing the regressed-state label; got:\n%s", body)
	}
}

// --- done-when: the reserved-but-empty clock-offset slot exists and shows
// nothing (§8.9's stated production deferral — never a fabricated number) ---

func TestStatusPageRendersEmptyClockOffsetSlotForEveryPeer(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-b",
			Peers:  []repl.PeerSnapshot{{SiteID: "site-a", LastContactAgoSec: f(4)}},
		}
	}
	body := getStatusPage(t, srv)
	if !strings.Contains(body, "clock offset:") || !strings.Contains(body, "reserved") {
		t.Fatalf("body missing the reserved clock-offset slot; got:\n%s", body)
	}
	// It must show NOTHING measured — no digit-bearing "Xms"/"Xs offset"
	// style value anywhere near the slot; the literal em-dash placeholder is
	// the only content.
	idx := strings.Index(body, "clock offset:")
	line := body[idx : idx+strings.Index(body[idx:], "</span>")]
	if !strings.Contains(line, "—") {
		t.Fatalf("clock offset slot did not render the empty-slot dash; got: %q", line)
	}
}

// --- not-done-if, HTTP level (belt-and-suspenders on top of the pure
// view-model test of the same name): a stale cached "ok" stream state must
// not read as healthy once the peer's last contact attempt errored ---

func TestStatusPageNeverRendersHealthyBoxWhilePeerLastErrorIsSet(t *testing.T) {
	srv := newTestServer(t)
	srv.SyncSnapshot = func() repl.Snapshot {
		return repl.Snapshot{
			SiteID: "site-b",
			Peers: []repl.PeerSnapshot{
				{SiteID: "site-a", LastContactAgoSec: f(120), LastError: "repl: GET /sync/streams: dial tcp: connect: connection refused"},
			},
			Streams: []repl.StreamStatus{
				// Stale cached OK from before the sever.
				{StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 1}, Watermark: 500, Head: 500, State: repl.StateOK},
			},
		}
	}
	body := getStatusPage(t, srv)
	if strings.Contains(body, `data-health="ok"`) {
		t.Fatalf("status page rendered a HEALTHY box for a peer whose last sync attempt failed — the exact silent-alerting-system defect §8.9 exists to prevent; body:\n%s", body)
	}
	if !strings.Contains(body, "FAILED") || !strings.Contains(body, "connection refused") {
		t.Fatalf("body did not surface the transport failure; got:\n%s", body)
	}
}
