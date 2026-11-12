// w7_y31_key_rejected_test.go — W7 / Y-31 (sc-test L12): a wrong peer key
// surfaced on /debug/sync-status as "401 Unauthorized: sync: bad or missing
// peer key" (correct, unchanged here) but in the log only as "PEER
// UNREACHABLE … check the link and the peer's own process" — the one
// diagnosis that could not be right: the peer answered. A 401 (our key
// refused) or 403 (our identity refused) is a configuration mismatch on a
// working link, and now gets its own gated line that says so, its own gate
// state so it transition-logs against "unreachable", and its own clearing
// line. Confirmed to FAIL against ecb0d51 (logged PEER UNREACHABLE, no
// PEER KEY REJECTED) and PASS after.
package repl

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/config"
)

func TestAPeerRefusingOurKeyIsLoggedAsAKeyRejectionNotAnOutageAndClearsWhenAccepted(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	ctx := context.Background()

	lh := &lateHandler{}
	lh.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sync: bad or missing peer key", http.StatusUnauthorized) // exactly what requireKey answers
	}))
	srv := httptest.NewServer(lh)
	t.Cleanup(srv.Close)
	eng, _ := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-a", BaseURL: srv.URL, Key: "demo-key-site-a"})

	eng.PullAll(ctx)
	out := buf.String()
	for _, want := range []string{"PEER KEY REJECTED", "401", "site-a", "not an outage", "CONFIGURATION mismatch"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Y-31: the log does not say %q after a 401 from the peer:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PEER UNREACHABLE") {
		t.Fatalf("a 401 is an ANSWER from a reachable peer, yet the log calls it unreachable:\n%s", out)
	}
	// The status surface's exact text is unchanged (the L12 scripts and the
	// status page key on it).
	if le := peerSnapOf(t, eng, "site-a").LastError; !strings.Contains(le, "401 Unauthorized: sync: bad or missing peer key") {
		t.Fatalf("last_error = %q, want the exact 401 text", le)
	}

	// Steady state: the same refusal every cycle logs nothing new (F14 gate;
	// the heartbeat is 5 minutes).
	eng.PullAll(ctx)
	eng.PullAll(ctx)
	out = buf.String()
	if n := countOccurrences(out, "PEER KEY REJECTED"); n != 1 {
		t.Fatalf("PEER KEY REJECTED logged %d times over three cycles, want exactly 1:\n%s", n, out)
	}
	if n := countOccurrences(out, "STILL REFUSING"); n != 0 {
		t.Fatalf("heartbeat line fired %d times within seconds, want 0:\n%s", n, out)
	}

	// The key is corrected (here: the peer starts accepting) → one clearing
	// line, and last_error is gone.
	realA, _ := engineWithPeers(t, "site-a") // PeerKey "demo-key-site-a" — what B is configured to send
	lh.set(realA.SyncHandler())
	eng.PullAll(ctx)
	out = buf.String()
	if !strings.Contains(out, "PEER ACCEPTS THIS POST AGAIN") || !strings.Contains(out, "sync resumed") {
		t.Fatalf("no clearing line after the peer accepted the key:\n%s", out)
	}
	if le := peerSnapOf(t, eng, "site-a").LastError; le != "" {
		t.Fatalf("last_error after recovery = %q, want empty", le)
	}
	if n := countOccurrences(out, "PEER REACHABLE AGAIN"); n != 0 {
		t.Fatalf("the recovery from a key refusal was logged as a reachability recovery:\n%s", out)
	}
}

func TestAPeerRefusingOurIdentityIsLoggedAsAConfigurationMismatchNotAnOutage(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `sync: this site serves only its own stream "site-x", not "site-a"`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	eng, _ := engineWithPeers(t, "site-b", config.Peer{SiteID: "site-a", BaseURL: srv.URL, Key: "k"})
	eng.PullAll(context.Background())
	out := buf.String()
	for _, want := range []string{"PEER KEY REJECTED", "403", "REFUSED THIS POST'S IDENTITY", "site-b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the log does not say %q after a 403 from the peer:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PEER UNREACHABLE") {
		t.Fatalf("a 403 is an ANSWER from a reachable peer, yet the log calls it unreachable:\n%s", out)
	}
}
