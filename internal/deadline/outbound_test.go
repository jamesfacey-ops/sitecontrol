package deadline

// outbound_test.go — NEW-a (James, 2026-10-03): THE SENDING POST SEES ITS OWN
// OVERDUE OUTBOUND TRUCKS. Until this change only the destination scanned a
// trip (OnApplied captures trips bound HERE), so the origin's crew learned a
// truck was late only by radio (L08 T-L08-B). The origin's time basis is the
// one the road-work scan established: the dispatch envelope's own authored_at
// (captureOwnAuthored). Two REAL stores stand in for the two posts, with
// envelopes carried between them through store.Apply — the engine's own
// apply path — so "an ack at either post clears both" is shown on two
// databases, not inferred from one.
//
// Failability: every test here was run against ecb0d51's scan (which holds no
// outbound rows at all) and failed on its first assertion — findAlert cannot
// find a row the origin never had.

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// scannerFor is newScanner for a chosen post — the origin's box, here.
func scannerFor(t *testing.T, st *store.Store, siteID string) *Deadlines {
	t.Helper()
	d, err := New(st, siteID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// replicate carries every event of `site`'s stream held by `from` into `to`
// through the store's own apply path (idempotent: already-applied envelopes
// are dropped by the apply loop). This is what the engine does on a pull,
// minus the transport.
func replicate(t *testing.T, from, to *store.Store, site string) {
	t.Helper()
	envs, err := from.Events(site, 1, 1, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) == 0 {
		t.Fatalf("precondition: %s holds no events on %s's stream to replicate", from.Path, site)
	}
	for _, env := range envs {
		if _, err := to.Apply(env); err != nil {
			t.Fatalf("apply %v: %v", env.Key(), err)
		}
	}
}

// rosterDriver creates a real roster person on the ORIGIN's stream — the
// arrival door that closes a run refuses a dangling driver id, and a person
// authored on origin's stream reaches the destination with the dispatch.
func rosterDriver(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	p, err := domain.CreatePerson(st, origin, 1, name, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p.PersonID
}

// outboundFixture: one A->B trip of X minutes in `st`, the origin's scanner,
// the trip and its envelope — and the precondition that the trip really is
// outbound from `origin` to `here` (rule 6: a fixture change that re-pointed
// it would make every assertion below vacuous).
func outboundFixture(t *testing.T, st *store.Store, xMinutes int64, driver string) (*Deadlines, domain.Trip, store.Envelope) {
	t.Helper()
	dA := scannerFor(t, st, origin)
	trip, env := dispatchInbound(t, st, xMinutes, driver)
	if trip.OriginSite != origin || trip.DestinationSite == nil || *trip.DestinationSite != here {
		t.Fatalf("precondition: trip is %s -> %v, want %s -> %s", trip.OriginSite, trip.DestinationSite, origin, here)
	}
	if time.UnixMilli(env.AuthoredAt.WallMS).Before(sanityFloor) {
		t.Fatalf("precondition: authored_at %d is before the sanity floor", env.AuthoredAt.WallMS)
	}
	return dA, trip, env
}

func TestTheOriginScansItsOwnOutboundTripOnItsOwnAuthoredAtAndRaisesTheRedWhenTheAllowancePasses(t *testing.T) {
	st := newSite(t)
	dA, trip, env := outboundFixture(t, st, 60, "driver-out")
	authored := time.UnixMilli(env.AuthoredAt.WallMS)

	// Half-way: a countdown, marked OUTBOUND, bound for the destination.
	dA.WallNow = func() time.Time { return authored.Add(30 * time.Minute) }
	board := mustEval(t, dA)
	a := findAlert(t, board, trip.TripID)
	if !a.Outbound || a.Destination != here || a.Origin != origin || a.RoadWork {
		t.Fatalf("row = outbound %v dest %q origin %q roadwork %v — want an outbound row from %s to %s", a.Outbound, a.Destination, a.Origin, a.RoadWork, origin, here)
	}
	if a.Status != StatusCountdown || a.Remaining < 25*time.Minute || a.Remaining > 35*time.Minute {
		t.Fatalf("at 30 of 60 min: %q remaining %s (%s), want a countdown of about 30 min", a.Status, a.Remaining, a.Reason)
	}
	if n := len(board.Alerts); n != 1 {
		t.Fatalf("the origin's board has %d rows for one trip, want exactly 1 (never an inbound row for its own dispatch)", n)
	}
	if len(board.Banner()) != 0 {
		t.Fatal("a healthy outbound countdown raised a banner row")
	}
	// The time basis is the own-authored one: receipt == authored_at, G == 0,
	// X == the allowance. Nothing invented.
	k := key{origin: env.SiteID, epoch: env.NodeEpoch, seq: env.Seq}
	c := captureRow(t, dA, k)
	if c.receiptWallMs != env.AuthoredAt.WallMS || !c.anchorOK || !c.gMs.Valid || c.gMs.Int64 != 0 || !c.xReceiptMs.Valid || c.xReceiptMs.Int64 != 60*60_000 {
		t.Fatalf("capture %+v — want receipt=authored_at (%d), anchor_ok, G=0, X=60 min", c, env.AuthoredAt.WallMS)
	}

	// Sixty-one minutes: a live red at the ORIGIN, whose staleness face names
	// the DESTINATION — the post whose unheard arrival record is what would
	// make this red false.
	dA.WallNow = func() time.Time { return authored.Add(61 * time.Minute) }
	board = mustEval(t, dA)
	a = findAlert(t, board, trip.TripID)
	if a.Status != StatusOverdueLive || a.OverdueBy < 30*time.Second || a.OverdueBy > 90*time.Second {
		t.Fatalf("at 61 min: %q by %s (%s), want overdue_live by about a minute", a.Status, a.OverdueBy, a.Reason)
	}
	if !strings.Contains(a.StalenessFace, "Last heard from "+here) || /* ⟦was: lower-case "last heard from" — FIX5: the face is its own sentence⟧ */ strings.Contains(a.StalenessFace, "from "+origin) {
		t.Fatalf("staleness face %q must name the destination %s, never this post", a.StalenessFace, here)
	}
	dA.Staleness = func(site string) (time.Duration, bool) {
		if site != here {
			t.Fatalf("staleness asked about %q, want the destination %s", site, here)
		}
		return 4 * time.Hour, true
	}
	if a = findAlert(t, mustEval(t, dA), trip.TripID); !strings.Contains(a.StalenessFace, "Last heard from "+here+": 4 hours ago") /* ⟦was: "last heard from …"⟧ */ {
		t.Fatalf("stale destination link must be on the face, got %q", a.StalenessFace)
	}
	dA.Staleness = func(string) (time.Duration, bool) { return 2 * time.Second, true }
	if a = findAlert(t, mustEval(t, dA), trip.TripID); a.StalenessFace != "" {
		t.Fatalf("current link must carry no staleness face, got %q", a.StalenessFace)
	}
	banner := mustEval(t, dA).Banner()
	if len(banner) != 1 || banner[0].TripID != trip.TripID || !banner[0].Outbound {
		t.Fatalf("banner = %+v, want the one outbound red", banner)
	}

	// The DESTINATION's scanner over the same store sees the same trip as
	// INBOUND, never outbound: one trip, one row per post, each in its own
	// direction.
	dB := newScanner(t, st)
	b := findAlert(t, mustEval(t, dB), trip.TripID)
	if b.Outbound || b.Destination != "" || b.Origin != origin {
		t.Fatalf("destination's row = %+v, want an inbound row from %s", b, origin)
	}
}

func TestAnAcknowledgmentAtEitherPostClearsTheOutboundRedAtBoth(t *testing.T) {
	// Two posts, two databases. A dispatches; its stream replicates to B.
	stA := newSite(t)
	dA, trip, env := outboundFixture(t, stA, 1, rosterDriver(t, stA, "Outbound Driver"))
	stB := newSite(t)
	dB := scannerFor(t, stB, here)
	replicate(t, stA, stB, origin)
	// B's capture, as the engine would take it on the pull: the link was
	// down longer than the whole allowance, so B's row is the §9.3 review
	// lane — a banner row at B of a different colour from A's live red.
	dB.OnApplied(env, time.Now().Add(-3*time.Minute), true)
	authored := time.UnixMilli(env.AuthoredAt.WallMS)
	dA.WallNow = func() time.Time { return authored.Add(2 * time.Minute) }

	if a := findAlert(t, mustEval(t, dA), trip.TripID); a.Status != StatusOverdueLive || !a.Outbound {
		t.Fatalf("precondition: A's row %q outbound %v, want a live outbound red", a.Status, a.Outbound)
	}
	if b := findAlert(t, mustEval(t, dB), trip.TripID); b.Status != StatusOverdueUnresolved || b.Outbound {
		t.Fatalf("precondition: B's row %q outbound %v, want the inbound review lane", b.Status, b.Outbound)
	}
	if len(mustEval(t, dA).Banner()) != 1 || len(mustEval(t, dB).Banner()) != 1 {
		t.Fatal("precondition: both posts must carry one banner row before any ack")
	}

	// B acknowledges on ITS stream; the ack replicates to A.
	by := "Ice Pad"
	if err := Acknowledge(stB, here, 1, trip.TripID, &by, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if len(mustEval(t, dB).Banner()) != 0 {
		t.Fatal("B's own ack left B's banner up")
	}
	if len(mustEval(t, dA).Banner()) != 1 {
		t.Fatal("precondition: before the ack replicates, A's banner must still be up — otherwise the clear below proves nothing")
	}
	replicate(t, stB, stA, here)
	boardA := mustEval(t, dA)
	a := findAlert(t, boardA, trip.TripID)
	if !a.Acked || !strings.Contains(a.AckFace, "Ice Pad") || len(boardA.Banner()) != 0 {
		t.Fatalf("after B's ack replicated: A's row Acked=%v AckFace=%q banner=%d — want acked, attributed to Ice Pad, banner clear", a.Acked, a.AckFace, len(boardA.Banner()))
	}

	// The other direction: a second truck, acked at A, clears B.
	trip2, env2 := dispatchInbound(t, stA, 1, rosterDriver(t, stA, "Second Driver"))
	replicate(t, stA, stB, origin)
	dB.OnApplied(env2, time.Now().Add(-3*time.Minute), true)
	dA.WallNow = func() time.Time { return time.UnixMilli(env2.AuthoredAt.WallMS).Add(2 * time.Minute) }
	if len(mustEval(t, dA).Banner()) != 1 || len(mustEval(t, dB).Banner()) != 1 {
		t.Fatal("precondition: the second truck must alert at both posts")
	}
	byA := "Ice Road"
	if err := Acknowledge(stA, origin, 1, trip2.TripID, &byA, 0, "default"); err != nil {
		t.Fatal(err)
	}
	replicate(t, stA, stB, origin)
	boardB := mustEval(t, dB)
	b := findAlert(t, boardB, trip2.TripID)
	if !b.Acked || !strings.Contains(b.AckFace, "Ice Road") || len(boardB.Banner()) != 0 {
		t.Fatalf("after A's ack replicated: B's row Acked=%v AckFace=%q banner=%d — want acked, attributed to Ice Road, banner clear", b.Acked, b.AckFace, len(boardB.Banner()))
	}

	// And the arrival, recorded at B, closes the run at A once it replicates.
	if _, err := domain.RecordExpectedArrival(stB, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	replicate(t, stB, stA, here)
	for _, row := range mustEval(t, dA).Alerts {
		if row.TripID == trip.TripID {
			t.Fatalf("the arrived trip is still on A's board: %+v", row)
		}
	}
}
