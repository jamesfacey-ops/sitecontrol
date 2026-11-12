// observability_test.go — F14 regression cover: sustained sync errors used
// to be visible ONLY on the status page (repl.Snapshot / /debug/sync-status)
// and never in journalctl — only REJOIN-time messages were ever logged (see
// Rejoin's own log.Printf calls). An operations specialist deliberately
// induced a real, sustained protocol error and confirmed journalctl showed
// nothing for the whole window, while demo-walkthrough.md's own
// troubleshooting sequence sends an operator to journalctl third.
//
// These tests pin two properties together, because either alone is not the
// fix: (1) a SUSTAINED failure produces a log line at all, and (2) repeated
// pull cycles over the SAME unchanged failure do not repeat that line every
// cycle (the "peer down for an hour must not write a line every 10 seconds"
// requirement) — logGate's state-transition-or-heartbeat discipline.
package repl

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

func countOccurrences(haystack, needle string) int {
	n := 0
	for {
		i := strings.Index(haystack, needle)
		if i < 0 {
			return n
		}
		n++
		haystack = haystack[i+len(needle):]
	}
}

// A peer that never answers, pulled across several consecutive cycles
// (simulating the routine ticker firing repeatedly while the peer stays
// down), must log the unreachable condition — but exactly once, not once
// per cycle.
func TestSustainedPeerUnreachableLogsOnceNotPerCycle(t *testing.T) {
	eng, _ := engineWithPeers(t, "site-b",
		// Nothing listens here — every exchange to this peer fails, same
		// pattern rejoin_test.go uses for an unreachable peer.
		config.Peer{SiteID: "site-x", BaseURL: "http://127.0.0.1:1", Key: "k"})

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		eng.PullAll(ctx) // 4 consecutive "sync cycles" against the same down peer
	}

	out := buf.String()
	if !strings.Contains(out, "site-x") || !strings.Contains(out, "UNREACHABLE") {
		t.Fatalf("4 cycles against a down peer logged nothing actionable; got:\n%s", out)
	}
	if !strings.Contains(out, "/debug/sync-status") {
		t.Fatalf("log line does not point an operator at the status page for detail; got:\n%s", out)
	}
	if n := countOccurrences(out, "UNREACHABLE"); n != 1 {
		t.Fatalf("4 cycles against the SAME unchanged down peer logged %d lines, want exactly 1 (no per-cycle flood); got:\n%s", n, out)
	}
}

// A terminal per-stream error (schema mismatch — §8.5's visible refusal),
// re-encountered across several consecutive pull cycles because the stall
// never clears, must log — but exactly once, not once per cycle.
func TestSustainedStreamErrorLogsOnceNotPerCycle(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 4})
	r.setEvents(1, mkStream("site-x", 1, 4, store.SchemaVersion+1, "site-x")) // foreign schema: terminal stall every cycle
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, _ := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		eng.PullAll(ctx)
	}

	if ss, ok := streamState(eng, "site-x", 1); !ok || ss.State != StateSchemaMismatch {
		t.Fatalf("test premise broken: stream state = %+v, want %s", ss, StateSchemaMismatch)
	}

	out := buf.String()
	if !strings.Contains(out, "site-x") || !strings.Contains(out, "SYNC ERROR") || !strings.Contains(out, StateSchemaMismatch) {
		t.Fatalf("4 cycles of a sustained schema-mismatch stall logged nothing actionable; got:\n%s", out)
	}
	if !strings.Contains(out, "/debug/sync-status") {
		t.Fatalf("log line does not point an operator at the status page for detail; got:\n%s", out)
	}
	if n := countOccurrences(out, "SYNC ERROR"); n != 1 {
		t.Fatalf("4 cycles over the SAME unchanged stream error logged %d lines, want exactly 1 (no per-cycle flood); got:\n%s", n, out)
	}
}

// The epoch-major hold (F14, new as of 0c06198): a stalled early epoch
// silently holds back a site's LATER epochs, which now stalls the whole
// SITE's ingest — this must not be silent either, and must not flood on
// repeated cycles over the same unresolved hold.
func TestSustainedEpochMajorHoldIsLoggedOnceNotPerCycle(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 4}, StreamHead{NodeEpoch: 2, Head: 3})
	r.setEvents(1, mkStream("site-x", 1, 4, store.SchemaVersion+1, "site-x")) // epoch 1: terminal stall
	r.setEvents(2, mkStream("site-x", 2, 3, store.SchemaVersion, "site-x"))   // epoch 2: healthy, but held back
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"})

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		eng.PullAll(ctx)
	}

	if wm := mustWatermark(t, st, "site-x", 2); wm != 0 {
		t.Fatalf("test premise broken: epoch 2 applied despite the epoch-1 stall, watermark=%d", wm)
	}

	out := buf.String()
	if !strings.Contains(out, "EPOCH HOLD") || !strings.Contains(out, "site-x") {
		t.Fatalf("4 cycles of a sustained epoch-major hold logged nothing actionable; got:\n%s", out)
	}
	if n := countOccurrences(out, "EPOCH HOLD"); n != 1 {
		t.Fatalf("4 cycles over the SAME unresolved hold logged %d lines, want exactly 1 (no per-cycle flood); got:\n%s", n, out)
	}
}
