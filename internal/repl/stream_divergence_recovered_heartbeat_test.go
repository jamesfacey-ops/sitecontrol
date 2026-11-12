// stream_divergence_recovered_heartbeat_test.go — SYNC-HONESTY (2026-09-14,
// STREAM-DIVERGENCE.md side finding): a per-stream logGate keyed on the
// literal state string "ok" (never ""), unlike the peer-error/epoch-hold
// gates, which both pass "" for their healthy/cleared state. Without a
// guard in setStream, a stream that reports ok every cycle still calls
// logGate.shouldLog("ok", ...) every cycle, so once
// sustainedErrorLogInterval elapses the "same state persists" branch fires
// and re-logs "SYNC RECOVERED ... (was ok)" for a stream that never left
// ok — measured on BOTH live demo posts, every ~5 minutes, since 09-13.
//
// Backdates the gate's lastLogAt directly (same package, no real sleep) to
// exercise the interval boundary deterministically.
package repl

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func TestHealthyStreamHeartbeatNeverRepeatsRecoveredButARealTransitionStillLogsIt(t *testing.T) {
	eng, _ := engineWithPeers(t, "site-b") // no peers needed: setStream touches no store/peer state
	key := StreamKey{SiteID: "site-a", NodeEpoch: 1}

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	// First ever ok: a genuine transition ("" -> "ok") and must log once.
	eng.setStream(key, 5, 5, StateOK, "")
	if n := strings.Count(buf.String(), "SYNC RECOVERED"); n != 1 {
		t.Fatalf("first-ever ok logged %d RECOVERED lines, want 1; got:\n%s", n, buf.String())
	}
	buf.Reset()

	// Stream keeps reporting ok every cycle, well within the heartbeat
	// interval — must stay silent (pre-existing behaviour; not the bug).
	eng.setStream(key, 6, 6, StateOK, "")
	eng.setStream(key, 7, 7, StateOK, "")
	if buf.Len() != 0 {
		t.Fatalf("ok persisting within the interval logged something; got:\n%s", buf.String())
	}

	// Backdate the gate past sustainedErrorLogInterval — models the routine
	// ticker firing every 10s for 5+ minutes over an unbroken-healthy
	// stream, without an real sleep.
	e := eng
	e.mu.Lock()
	g := e.streamLog[key]
	if g == nil || g.lastState != StateOK {
		e.mu.Unlock()
		t.Fatalf("fixture: gate state = %+v, want lastState=%q after two healthy calls", g, StateOK)
	}
	g.lastLogAt = time.Now().Add(-sustainedErrorLogInterval - time.Second)
	e.mu.Unlock()

	// THE BUG: reporting ok again, still ok, after the interval elapsed.
	eng.setStream(key, 8, 8, StateOK, "")
	if out := buf.String(); strings.Contains(out, "SYNC RECOVERED") {
		t.Fatalf("STREAM-DIVERGENCE side finding reproduced: a healthy stream (ok -> ok) re-logged RECOVERED after the heartbeat interval elapsed, training an operator to ignore the one line that means something; got:\n%s", out)
	}
	buf.Reset()

	// A GENUINE error, then a genuine recovery, must still log both lines —
	// this fix must never silence a real transition.
	eng.setStream(key, 8, 9, StateError, "transport failure")
	if n := strings.Count(buf.String(), "SYNC ERROR"); n != 1 {
		t.Fatalf("genuine ok->error transition logged %d SYNC ERROR lines, want 1; got:\n%s", n, buf.String())
	}
	buf.Reset()

	eng.setStream(key, 9, 9, StateOK, "")
	if n := strings.Count(buf.String(), "SYNC RECOVERED"); n != 1 {
		t.Fatalf("genuine error->ok transition logged %d RECOVERED lines, want exactly 1 (must not be silenced); got:\n%s", n, buf.String())
	}
}
