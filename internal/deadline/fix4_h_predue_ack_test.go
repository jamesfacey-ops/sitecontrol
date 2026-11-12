package deadline

import (
	"strings"
	"testing"
	"time"
)

// TestFix4HTheFarEndsAmberAckDoesNotClaimTheOriginsCountdownRow — R6-1.6a
// (fix4 H). Before the due time, the destination acked its own amber; the
// origin's outbound COUNTDOWN row read "(ACKNOWLEDGED BY ICE PAD)" and offered
// "Un-acknowledge — put this alert back", though nothing on the origin's
// screen was silenced (silencesPlain: "amber-only silences unless red").
// FAIL BEFORE: Acked and Unackable were true on the countdown row.
func TestFix4HTheFarEndsAmberAckDoesNotClaimTheOriginsCountdownRow(t *testing.T) {
	st := newSite(t)
	dA, trip, env := outboundFixture(t, st, 60, "driver-1-6a")
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	by := "Ice Pad"
	if err := AcknowledgeKind(st, here, 1, trip.TripID, AckKindAmber, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	dA.WallNow = func() time.Time { return authored.Add(30 * time.Minute) }
	a := findAlert(t, mustEval(t, dA), trip.TripID)
	if a.Status != StatusCountdown || !a.Outbound {
		t.Fatalf("precondition: want the origin's outbound countdown row, got %q outbound %v", a.Status, a.Outbound)
	}
	if a.Acked || a.Unackable() || a.AckFace != "" {
		t.Errorf("the origin's countdown row claims the far end's amber ack: acked %v unackable %v face %q", a.Acked, a.Unackable(), a.AckFace)
	}
	if !strings.Contains(a.VerifyOnlyAckFace, "acknowledged at Ice Pad as VERIFY only") || strings.Contains(a.VerifyOnlyAckFace, "this red") {
		t.Errorf("VerifyOnlyAckFace on a countdown = %q, want the VERIFY-only face without calling the row red", a.VerifyOnlyAckFace)
	}
	// At the due time the row goes red, still unacknowledged — unchanged (F4).
	dA.WallNow = func() time.Time { return authored.Add(61 * time.Minute) }
	b := mustEval(t, dA)
	a = findAlert(t, b, trip.TripID)
	if a.Status != StatusOverdueLive || a.Acked || a.Unackable() || len(b.Banner()) != 1 {
		t.Errorf("after the due: %q acked %v unackable %v banner %d", a.Status, a.Acked, a.Unackable(), len(b.Banner()))
	}
	if a.VerifyOnlyAckFace != "acknowledged at Ice Pad as VERIFY only — this red is not acknowledged" {
		t.Errorf("red row face %q", a.VerifyOnlyAckFace)
	}
}
