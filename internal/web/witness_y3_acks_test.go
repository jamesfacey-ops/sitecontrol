// witness_y3_acks_test.go — the witness phase (reports/witness/_BRIEF.md),
// worktree y3-acks, killing the census survivor P7
// (internal/web/unack.go:84) that lives in this package. See
// reports/witness/y3-acks.md for the run-both-ways record.
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestWitnessP7ReviewPhaseShapeGuardRefusesGarbage — P7
// (internal/web/unack.go:84):
// `if n, err := strconv.ParseInt(v, 10, 64); err != nil || n < 1 { ...400 }`.
//
// PLANT: `if n, err := strconv.ParseInt(v, 10, 64); false && (err != nil ||
// n < 1)` — the refusal never fires for ANY input. A non-numeric or
// non-positive `review_phase` no longer gets the 400: the field's mere
// PRESENCE already selects the phase scope (unack.go's own comment: "its
// VALUE selects nothing"), so the handler falls through and asks
// EffectiveAcks for a phase-scope target set anyway, and the officer's
// malformed submission is answered by whatever THAT returns (here, on a
// never-acked trip: 409, not the intended 400) instead of the refusal
// naming the actual problem.
// TestWitnessD5UnackHandlerSurfacesTheEffectiveAcksError — the "work
// outward" step past the census's 34 named plants, for
// internal/web/unack.go:93-96 (the handler's `deadline.EffectiveAcks`
// error arm; blocks19_unc.tsv). Not a census plant — authored for this
// pass; see reports/witness/y3-acks.md.
//
// A closed store makes EffectiveAcks itself fail (a whole-store fault is a
// valid discriminator HERE because, unlike EffectiveAcks' own two internal
// dump calls, this handler has only ONE call into the deadline package
// before authoring — there is no second, differently-wired call this arm's
// neighbour could be confused with).
//
// PLANT (applied and reverted for this witness — the wording above once
// read "PLANT (if applied)"; y3's own report says it WAS applied, and
// u1b-web re-applied and re-killed it, so the hedge was stale, not the
// claim): disabling this arm would let a database failure fall through into
// `len(targets) == 0`'s branch (targets is nil on the zero value), turning
// an INTERNAL ERROR into the officer-facing "nothing to un-acknowledge" 409
// — a database outage misreported as a fact about the trip's ack state.
//
// u1b-web §3.5 classified this AMBIGUOUS-BUT-UNPRODUCED: today's status-only
// assertion is a real kill (no OTHER store access sits upstream of
// EffectiveAcks on this route — grep confirms neither requireLogin nor
// currentSession touches the store), but that is blind to any future read
// added ahead of it, which would 500 for a different reason and still pass.
// The audit's discriminating form: capture the arm's OWN log line —
// `s.tripsFail(w, "unack: effective acknowledgments", err)` at unack.go:94
// prints "trips: unack: effective acknowledgments: <err>", unique to this
// call in the whole handler — and require it present, not merely a bare 500.
func TestWitnessD5UnackHandlerSurfacesTheEffectiveAcksError(t *testing.T) {
	srv, st, _, trip := newAlertingServer(t)
	if err := st.Close(); err != nil {
		t.Fatalf("fixture: closing the store: %v", err)
	}
	logBuf := w5aCaptureWebLog(t)

	rec := postForm(t, srv, "/alerts/unack", url.Values{"trip_id": {trip.TripID}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /alerts/unack on a closed store = %d, want 500 (tripsFail); body:\n%.400s", rec.Code, rec.Body.String())
	}
	const wantLog = "trips: unack: effective acknowledgments:"
	if !strings.Contains(logBuf.String(), wantLog) {
		t.Errorf("POST /alerts/unack on a closed store answered 500, but the log never carries %q — a 500 from a DIFFERENT read is not this arm caught (log has: %q)", wantLog, logBuf.String())
	}
}

// ⟦was: want "review_phase must be a positive phase number" — the shape
// guard on a still-accepted field. Weather phases removed, James 2026-10-04:
// review_phase is refused whatever its value, so every garbage value still
// gets a 400, now with the removal sentence.⟧
func TestWitnessP7ReviewPhaseShapeGuardRefusesGarbage(t *testing.T) {
	srv, _, _, trip := newAlertingServer(t)
	const want = "review_phase is no longer accepted"

	for _, bad := range []string{"notanumber", "0", "-1", "3.5"} {
		t.Run(bad, func(t *testing.T) {
			rec := postForm(t, srv, "/alerts/unack", url.Values{
				"trip_id":      {trip.TripID},
				"review_phase": {bad},
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /alerts/unack (review_phase=%q) = %d, want 400; body:\n%.400s", bad, rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); !strings.Contains(got, want) {
				t.Fatalf("POST /alerts/unack (review_phase=%q) body = %q, want it to contain %q", bad, got, want)
			}
		})
	}
}
