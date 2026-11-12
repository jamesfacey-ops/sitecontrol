// r3_ack_kind_test.go — ruling R3, second half (James, 2026-10-04): "an ack
// on an amber VERIFY row should not silence the other end's red overdue; ack
// per alert kind." L08 T-116: the pad acked its amber VERIFY BY RADIO row for
// a truck the origin was showing as a live red, and the trip-scoped ack
// silenced the origin's red too. SchemaVersion 16 carries the kind.
package deadline

import (
	"testing"
	"time"
)

func TestR3AnAmberAckDoesNotSilenceTheOtherEndsRedButARedAckClearsBoth(t *testing.T) {
	// Two posts, two databases: A dispatched, B's capture is the review lane
	// (amber) while A's own row is a live red — the T-116 arrangement.
	stA := newSite(t)
	dA, trip, env := outboundFixture(t, stA, 1, rosterDriver(t, stA, "Outbound Driver"))
	stB := newSite(t)
	dB := scannerFor(t, stB, here)
	replicate(t, stA, stB, origin)
	dB.OnApplied(env, time.Now().Add(-3*time.Minute), true)
	dA.WallNow = func() time.Time { return time.UnixMilli(env.AuthoredAt.WallMS).Add(2 * time.Minute) }
	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusOverdueLive {
		t.Fatalf("precondition: A's row %q, want a live red", a.Status)
	}
	if b := findAlert(t, mustEval(t, dB), trip.TripID); b.Status != StatusOverdueUnresolved {
		t.Fatalf("precondition: B's row %q, want amber (overdue_unresolved)", b.Status)
	}

	// B acks ITS amber row.
	byB := "Ice Pad"
	if err := AcknowledgeKind(stB, here, 1, trip.TripID, AckKindAmber, &byB, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if b := findAlert(t, mustEval(t, dB), trip.TripID); !b.Acked || len(mustEval(t, dB).Banner()) != 0 {
		t.Fatalf("B's own amber ack must silence B's amber row: Acked=%v", b.Acked)
	}
	replicate(t, stB, stA, here)
	boardA := mustEval(t, dA)
	if a := findAlert(t, boardA, trip.TripID); a.Acked || a.Status != StatusOverdueLive || len(boardA.Banner()) != 1 {
		t.Fatalf("R3: B's AMBER ack silenced A's RED: Acked=%v status=%q banner=%d — an amber ack must never silence a red", a.Acked, a.Status, len(boardA.Banner()))
	}
	if a := findAlert(t, boardA, trip.TripID); !a.HasStandingAck {
		t.Fatal("B's amber ack is still a standing ack on the record (un-ackable at A)")
	}

	// A acks its red: that clears A, and B stays clear.
	byA := "Ice Road"
	if err := AcknowledgeKind(stA, origin, 1, trip.TripID, AckKindRed, &byA, 0, "default"); err != nil {
		t.Fatal(err)
	}
	replicate(t, stA, stB, origin)
	if n := len(mustEval(t, dA).Banner()); n != 0 {
		t.Fatalf("A's red ack left %d banner rows at A", n)
	}
	if n := len(mustEval(t, dB).Banner()); n != 0 {
		t.Fatalf("B's banner came back after A's red ack: %d rows", n)
	}

	// The other way round: a second truck, RED ack at A alone clears B's amber.
	trip2, env2 := dispatchInbound(t, stA, 1, rosterDriver(t, stA, "Second Driver"))
	replicate(t, stA, stB, origin)
	dB.OnApplied(env2, time.Now().Add(-3*time.Minute), true)
	dA.WallNow = func() time.Time { return time.UnixMilli(env2.AuthoredAt.WallMS).Add(2 * time.Minute) }
	if err := AcknowledgeKind(stA, origin, 1, trip2.TripID, AckKindRed, &byA, 0, "default"); err != nil {
		t.Fatal(err)
	}
	replicate(t, stA, stB, origin)
	if b := findAlert(t, mustEval(t, dB), trip2.TripID); !b.Acked {
		t.Fatalf("R3: a RED ack at the origin must silence the destination's amber row too")
	}
}

func TestR3AcknowledgeKindRefusesAnUnknownKindAndAuthorsNothing(t *testing.T) {
	st := newSite(t)
	_, trip, _ := outboundFixture(t, st, 1, rosterDriver(t, st, "Driver"))
	before, err := st.Watermark(origin, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := AcknowledgeKind(st, origin, 1, trip.TripID, "countdown", nil, 0, "default"); err == nil {
		t.Fatal("an ack of kind \"countdown\" was accepted")
	}
	if after, _ := st.Watermark(origin, 1); after != before {
		t.Fatal("the refused ack authored an event")
	}
}
