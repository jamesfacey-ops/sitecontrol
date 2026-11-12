package deadline

import (
	"database/sql"
	"testing"
)

// TestFix4LegacyPhaseAckRowFoldedByTheOldBuildNeverSilencesARed is R6 L06
// P4c: a store folded by the pre-removal v16 build keeps the alert_ack ROW of
// a review_phase ack (old table shape: an extra review_phase column,
// alert_kind NULL). The fold now skips the event, but the store is not
// re-folded on open, so the row survived and read as a kind-less ack — the
// red was shown acknowledged at that post and un-acked at a fresh peer.
// FAIL BEFORE: the row silenced the red and was offered to un-ack.
func TestFix4LegacyPhaseAckRowFoldedByTheOldBuildNeverSilencesARed(t *testing.T) {
	d, tripID := fix3OverdueTrip(t)
	st := d.st
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The old build's table, as CREATE TABLE IF NOT EXISTS left it, and the
	// row its fold wrote for a phase-3 VERIFY-row ack.
	if _, err := db.Exec(`ALTER TABLE alert_ack ADD COLUMN review_phase INTEGER`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO alert_ack (site_id, node_epoch, seq, trip_id, recorded_by, alert_kind, review_phase)
		VALUES (?, 1, 999, ?, 'Ice Pad', NULL, 3)`, here, tripID); err != nil {
		t.Fatal(err)
	}
	b := mustEval(t, d)
	if a := findAlert(t, b, tripID); a.Status != StatusOverdueLive || a.Acked || a.HasStandingAck {
		t.Fatalf("with a legacy phase-ack row: status %q acked %v standing %v — the red must stand", a.Status, a.Acked, a.HasStandingAck)
	}
	if acks, err := EffectiveAcks(st, tripID); err != nil || len(acks) != 0 {
		t.Fatalf("EffectiveAcks = %v (err %v) — a legacy phase-ack row is not an ack to retract", acks, err)
	}
	// CONTROL: a plain row in the same old-shape table (review_phase NULL)
	// is an ordinary ack and does silence the red.
	if _, err := db.Exec(`INSERT INTO alert_ack (site_id, node_epoch, seq, trip_id, recorded_by, alert_kind, review_phase)
		VALUES (?, 1, 1000, ?, 'Ice Pad', NULL, NULL)`, here, tripID); err != nil {
		t.Fatal(err)
	}
	if a := findAlert(t, mustEval(t, d), tripID); !a.Acked {
		t.Fatalf("CONTROL: a plain old-shape row did not silence the red: %+v", a)
	}
}
