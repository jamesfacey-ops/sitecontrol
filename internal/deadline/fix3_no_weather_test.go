package deadline

// fix3_no_weather_test.go — weather phases removed (James, 2026-10-04: "the
// phase conditions can be removed they will not effect site control … go
// ahead and delete the phase conditions"), inside the unreleased
// SchemaVersion 16. A v16 log written before the removal may still hold the
// two weather-only event shapes; both are SKIPPED on read, never refused —
// a refusal would stop a post opening on a log it cannot rewrite:
//
//   - an `alert_acknowledged` carrying `review_phase` (a weather VERIFY-row
//     ack) folds to NOTHING — read as a plain ack it would silence the
//     trip's red, an under-alert through a side door;
//   - a `weather_phase_declared` has no fold: it applies (the log keeps it)
//     and changes no projection and no alert. It can no longer be AUTHORED.

import (
	"strings"
	"testing"
	"time"
)

// fix3OverdueTrip puts one inbound trip into a live red on a fresh scanner.
func fix3OverdueTrip(t *testing.T) (*Deadlines, string) {
	t.Helper()
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 1, "driver-1")
	d.OnApplied(env, time.Now().Add(-time.Minute+50*time.Millisecond), true) // X−G = 50ms
	time.Sleep(120 * time.Millisecond)
	if a := findAlert(t, mustEval(t, d), trip.TripID); a.Status != StatusOverdueLive || a.Acked {
		t.Fatalf("precondition: status %q acked %v, want an unacked live red", a.Status, a.Acked)
	}
	return d, trip.TripID
}

func TestFix3ALegacyPhaseScopedAckIsSkippedAndNeverSilencesARed(t *testing.T) {
	d, tripID := fix3OverdueTrip(t)
	st := d.st
	phase := int64(3)
	board := "2026-10-03T06:42:00"
	at := w9cKey{here, 1, w9cNextSeq(t, st, here, 1)}
	w9cApply(t, st, w9cAckEnvelope(at, tripID, &phase, &board, "Ice Pad", 1_700_000_000_000))

	b := mustEval(t, d)
	if a := findAlert(t, b, tripID); a.Status != StatusOverdueLive || a.Acked || a.HasStandingAck {
		t.Fatalf("after a legacy phase-scoped ack: status %q acked %v standing %v — the ack must be skipped, the red must stand", a.Status, a.Acked, a.HasStandingAck)
	}
	var onBanner bool
	for _, r := range b.Banner() {
		onBanner = onBanner || r.TripID == tripID
	}
	if !onBanner {
		t.Fatal("the red left the banner after a legacy phase-scoped ack")
	}
	if acks, err := EffectiveAcks(st, tripID); err != nil || len(acks) != 0 {
		t.Fatalf("EffectiveAcks = %v (err %v) — a skipped legacy ack must not be offered to un-ack either", acks, err)
	}

	// CONTROL: the same envelope shape WITHOUT review_phase is an ordinary
	// ack and does silence the row — so the skip above is the key's doing,
	// not a fixture that never folds at all.
	at = w9cKey{here, 1, w9cNextSeq(t, st, here, 1)}
	w9cApply(t, st, w9cAckEnvelope(at, tripID, nil, nil, "Ice Pad", 1_700_000_000_001))
	if a := findAlert(t, mustEval(t, d), tripID); !a.Acked {
		t.Fatalf("CONTROL: a plain ack in the same shape did not silence the row: %+v", a)
	}
}

func TestFix3ALegacyWeatherDeclarationReplaysInertAndCannotBeAuthored(t *testing.T) {
	d, tripID := fix3OverdueTrip(t)
	st := d.st
	before, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	decl := map[string]any{
		"type": "weather_phase_declared", "declaration_id": "site-b-phase-legacy",
		"phase": 3, "effective_at_board": "2026-10-03T06:42:00", "recorded_by": "Ice Road",
	}
	w9cApply(t, st, w9cEnvelope(w9cKey{here, 1, w9cNextSeq(t, st, here, 1)}, 1_700_000_000_000, decl))

	after, err := st.DumpProjections()
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a legacy weather_phase_declared changed the projections — it must replay inert")
	}
	if strings.Contains(string(after), "weather_phase") {
		t.Fatal("the dump still names weather_phase")
	}
	if a := findAlert(t, mustEval(t, d), tripID); a.Status != StatusOverdueLive || a.Acked {
		t.Fatalf("after a legacy declaration: status %q acked %v, want the red unchanged", a.Status, a.Acked)
	}

	// Authoring one is refused: the type left the G13 closed set.
	payload := []byte(`{"type":"weather_phase_declared","declaration_id":"site-b-phase-new","phase":3,"effective_at_board":"2026-10-04T06:00:00"}`)
	if _, err := st.Author(here, 1, payload, 0, "default"); err == nil {
		t.Fatal("a weather_phase_declared was AUTHORED — the type must be refused at authoring now")
	}
}

// TestFix3AnInboundAllowanceIsTheRecordedDeadlineUnscaled: with no phase
// concept the scan's allowance is the officer's recorded deadline, and the
// capture's x_receipt is that same figure.
func TestFix3AnInboundAllowanceIsTheRecordedDeadlineUnscaled(t *testing.T) {
	st := newSite(t)
	d := newScanner(t, st)
	trip, env := dispatchInbound(t, st, 65, "driver-1")
	d.OnApplied(env, time.Now(), true)
	a := findAlert(t, mustEval(t, d), trip.TripID)
	if a.Status != StatusCountdown || a.Deadline != 65*time.Minute {
		t.Fatalf("status %q deadline %s, want a 65-minute countdown", a.Status, a.Deadline)
	}
	if a.Remaining > 65*time.Minute || a.Remaining < 64*time.Minute {
		t.Fatalf("remaining %s, want just under 65 min", a.Remaining)
	}
	c := captureRow(t, d, key{origin: env.SiteID, epoch: env.NodeEpoch, seq: env.Seq})
	if !c.xReceiptMs.Valid || c.xReceiptMs.Int64 != 65*60_000 {
		t.Fatalf("x_receipt = %+v, want 65 min exactly", c.xReceiptMs)
	}
}
