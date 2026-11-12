// scan.go — the §9.4 server-side periodic scan and the §9.3 classification.
package deadline

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/humantime"
)

// Status of one open inbound deadline expectation.
type Status string

const (
	// StatusCountdown: X−G remains; counting on this box's monotonic clock.
	StatusCountdown Status = "countdown"
	// StatusOverdueLive: the countdown expired here, live — the banner alert.
	StatusOverdueLive Status = "overdue_live"
	// StatusOverdueUnresolved: the §9.3 review lane — deadline credibility
	// was lost (expired during a partition, unknown G, replay, bad clock).
	// NEVER a live alert and NEVER a fresh countdown.
	StatusOverdueUnresolved Status = "overdue_unresolved"
	// ⟦was: StatusPhaseReview ("phase_review") — the weather-phase in-transit
	// VERIFY row and the F39 post-rescale row; removed with weather phases,
	// James 2026-10-04. The amber VERIFY row that remains is
	// StatusOverdueUnresolved (sync anchor, replay, R9 clock change).⟧
)

// Alert is one classified expectation — everything the banner and board need.
type Alert struct {
	TripID    string
	Vehicle   string // verbatim entered form, always (M8)
	Origin    string
	Driver    string
	Occupants int
	Deadline  time.Duration // X

	Status    Status
	Acked     bool          // an alert_ack event exists — banner dismissed
	Remaining time.Duration // countdown only
	OverdueBy time.Duration // overdue_live only
	Reason    string        // overdue_unresolved only — the review framing
	// DeadlineUnknown (FINDINGS-R4 D15): an overdue_unresolved row whose
	// allowance is NOT known to have passed — this post simply cannot count
	// it (replay, no receipt record, no sync anchor at receipt, a changed
	// clock). False on the one review branch that does know: the deadline
	// passed during a partition (branch 4). Surfaces that would otherwise
	// say "past deadline" read this instead of guessing.
	DeadlineUnknown bool
	// ClockChanged (R9, James 2026-10-04): the review lane was entered
	// because this box's own wall clock cannot be trusted across the
	// bridge — implausible, gone backwards, or stepped against its uptime
	// (clockstep.go). Surfaces name it: "clock changed since receipt —
	// verify by radio", never OVERDUE.
	ClockChanged bool
	// DueAt (Q-FE2 — James, 2026-10-04: "no-anchor/clock rows stay AMBER,
	// relabel VERIFY BY RADIO, show recorded due time, turn RED when that due
	// time passes"): the run's RECORDED due instant — its effective
	// departure, on the ORIGIN's clock, plus the allowance. James
	// 2026-10-06: from the typed time (domain.EffectiveDeparture: the
	// dispatch's typed time, else its authored_at) ⟦was: FINDINGS-R4 D13,
	// the authored_at alone⟧ — the same basis third-post site pages use. Set on the
	// review rows whose due time is on record (no sync anchor at receipt, a
	// changed clock, a deadline passed during a partition) and on the red
	// those rows turn into; zero elsewhere. Surfaces render it as "due about
	// 11:07 by <origin>'s clock" (DueClock), and the alert strip orders its
	// ambers by it, soonest first.
	DueAt time.Time
	// StalenessFace is §9.3 rule 3 / A1d: a LIVE alert raised while current
	// staleness to the origin exceeds the routine sync interval carries that
	// staleness on its face. Empty when the link is current.
	StalenessFace string

	// ⟦was: PhaseFace, OverrideFace and ReviewPhase — the rescale face, the
	// phase-override face and the phase VERIFY row's scope; removed with
	// weather phases, James 2026-10-04⟧

	// AckFace (F6 §5 R1, W9c item 17): "acknowledged by <post>" on an ACKED
	// row, from the effective ack's recorded_by/site_id. Empty on an
	// unacked row. Until F6 the acked row carried a bare " (acknowledged)"
	// suffix with no attribution at all, so a post that un-acked and then
	// saw the row go quiet again — because the peer's independent re-ack
	// crossed in flight — could not tell that from "my un-ack did nothing".
	AckFace string
	// RepudiationFace (F6 §5 R2): "previously acknowledged by X,
	// un-acknowledged by Y", owed wherever the board renders a row for a
	// trip carrying a retraction of one of that row's acks — WHETHER OR NOT
	// the trip is currently acked. The un-acked case is the one it exists
	// for: without it a returned-to-unacknowledged red row renders
	// byte-identical to a never-acknowledged one, and the officer who acked
	// ten minutes ago re-acks without ever being shown that a peer
	// repudiated it. No stored status — recomputed on every Evaluate from
	// the same two projections the §3.3 predicate already reads.
	RepudiationFace string
	// HasStandingAck: an EFFECTIVE (unretracted) acknowledgment exists in
	// THIS row's scope — exactly the condition under which POST
	// /alerts/unack would author rather than refuse. It is NOT the same as
	// Acked: under R3 an amber-only ack is standing on a row that has since
	// gone red, so the row alerts (Acked == false) while a standing human
	// act is still on the record. ⟦was: the dormant-phase-ack case — "a
	// phase-2 ack is DORMANT at phase 3" — removed with weather phases,
	// James 2026-10-04⟧
	HasStandingAck bool
	// VerifyOnlyAckFace (FINDINGS-R5 F4): set exactly when an ack stands on
	// the record but does NOT silence this row — under R3 that is an
	// amber-only ack on a row that is red here: "acknowledged at Ice Pad as
	// VERIFY only — this red is not acknowledged" — or still counting down
	// here (R6-1.6a: the origin's pre-due row after the far end's amber
	// ack). Such a row offers no
	// un-ack control (Unackable), because "put this alert back" would be
	// false on a row that is still alerting, and pressing it would re-arm the
	// far end's amber.
	VerifyOnlyAckFace string

	// RoadWork (James, 2026-10-03) marks a row for equipment WORKING ON THE
	// ROAD (domain/road_work.go): Deadline is the origin post's configured
	// working LIMIT rather than a journey allowance, Origin is THIS post (the
	// origin owns the run — see Evaluate), and the banner words it as
	// equipment out past its limit, never as an inbound vehicle overdue. The
	// statuses and the ack are the same machinery; only the time basis
	// (own-authored — captureOwnAuthored) differs. ⟦was: "The four statuses,
	// the ack, the phase VERIFY row and the F39 red-fired marker … and the
	// absence of phase scaling differ" — weather phases removed 2026-10-04⟧
	RoadWork bool

	// Outbound (James, 2026-10-03 — NEW-a: "the sending post sees its own
	// overdue outbound trucks") marks a row for a controlled run THIS post
	// SENT OUT: Origin is this post, Destination is the post it is bound
	// for. Until today only the destination scanned a trip (OnApplied
	// captures trips bound HERE), so the crew who dispatched a truck learned
	// it was late only by radio (L08 T-L08-B). The origin's time basis is the
	// one the road-work scan established this morning — the dispatch
	// envelope's own authored_at, G = 0 (captureOwnAuthored), less the typed
	// lead (James 2026-10-06: from the typed time — classify) — so nothing is
	// invented and no clock is compared across posts: each end counts on its
	// own clock from the instant it recorded. Same statuses, same ack
	// (trip-scoped and replicated, so an acknowledgment at EITHER post clears
	// the row at both). What differs: the staleness face names the
	// destination, because the fact this post may be missing is the arrival
	// recorded there. ⟦was: "same phase VERIFY row, same F39 marker. What
	// differs: the travel-table segment is the DESTINATION …" — weather
	// phases removed 2026-10-04⟧
	Outbound    bool
	Destination string // the post the run is bound for; "" on an inbound or road-work row
}

// farPost is the post whose link this row depends on — the one whose silence
// could mean an arrival this post has not heard about: the origin of an
// inbound run, the destination of an outbound one.
func (a Alert) farPost() string {
	if a.Outbound {
		return a.Destination
	}
	return a.Origin
}

// Unackable (FINDINGS-R5 F4): the un-ack control belongs on this row only
// when a standing ack actually SILENCES it here. ⟦was: the boards offered it
// on HasStandingAck — "an ack stands for this trip" — so a red row whose only
// ack was the far end's amber ack showed "Un-acknowledge — put this alert
// back" (false: nothing was silenced; pressing it re-armed the far amber).⟧
func (a Alert) Unackable() bool { return a.Acked && a.HasStandingAck }

// DueClock renders DueAt as an officer reads a clock ("11:07"), in this
// box's local zone; "" when no due time is on record.
func (a Alert) DueClock() string {
	if a.DueAt.IsZero() {
		return ""
	}
	return a.DueAt.In(time.Local).Format("15:04")
}

// NoReceiptTimingLabel is the Q-FE2 row label (James, 2026-10-04) for a
// review row this post cannot count: no sync anchor at receipt, no receipt
// record, a replay. "OVERDUE" must not appear on it — for most of the
// allowance the run is not overdue at all. ⟦was: "OVERDUE — UNRESOLVED"⟧
const NoReceiptTimingLabel = "VERIFY BY RADIO — no receipt timing at this post"

// ReviewLabel is the board label for an overdue_unresolved row, one spelling
// for every board: a changed clock (R9), no receipt timing (Q-FE2), or —
// the one review row whose deadline is KNOWN to have passed, during a
// partition (branch 4) — "OVERDUE — UNRESOLVED".
func (a Alert) ReviewLabel() string {
	switch {
	case a.ClockChanged:
		return ClockChangedLabel
	case a.DeadlineUnknown:
		return NoReceiptTimingLabel
	default:
		return "OVERDUE — UNRESOLVED"
	}
}

// LimitText renders Deadline the way an officer says a working limit — "12 h",
// "1 h 30 min", "2 min" — for the road-work banner rows. Journey allowances
// keep their own minute spellings elsewhere; this is the one place an hours
// figure is the natural unit.
func (a Alert) LimitText() string { return limitText(a.Deadline) }

func limitText(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int64(d / time.Hour)
	m := int64((d % time.Hour) / time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d h %d min", h, m)
	case h > 0:
		return fmt.Sprintf("%d h", h)
	default:
		return fmt.Sprintf("%d min", m)
	}
}

// stampAckFaces puts F6 §5's two clauses on one row. R1 is owed on an ACKED
// row (an unacked row has no standing acknowledgment to attribute); R2 is
// owed on ANY row for a trip carrying a repudiated ack —
// the un-acked row included, which is the whole of what rev 4 widened.
func stampAckFaces(a *Alert, f *ackFaces) {
	if f == nil {
		return
	}
	a.HasStandingAck = len(f.acknowledgedBy) > 0
	if a.Acked {
		a.AckFace = f.ackFace()
	} else if a.HasStandingAck {
		// F4: a standing ack that does not silence this row — amber-only
		// acks on a row that is not amber here: red, or (R6-1.6a) still
		// counting down, where the red to come is not acknowledged either.
		tail := " as VERIFY only — this red is not acknowledged"
		if a.Status != StatusOverdueLive {
			tail = " as VERIFY only — that does not acknowledge this run if it goes overdue"
		}
		a.VerifyOnlyAckFace = "acknowledged at " + joinNames(dedupe(f.acknowledgedBy)) + tail
	}
	a.RepudiationFace = f.repudiationFace()
}

// Board is one scan's output, in source-key order.
type Board struct {
	Alerts []Alert
	// ⟦was: PhaseReview []Alert — the weather-phase in-transit VERIFY rows
	// (§7); removed with weather phases, James 2026-10-04⟧
}

// Banner returns the rows every session must surface until arrival check-in
// or an explicit logged acknowledgment (§9.4): live overdue alerts plus
// overdue-unresolved review entries, unacked. ⟦was: "— and, while a
// review_in_transit phase is current, the per-trip phase VERIFY rows …" —
// weather phases removed, James 2026-10-04⟧
func (b Board) Banner() []Alert {
	var out []Alert
	for _, a := range b.Alerts {
		if a.Acked || a.Status == StatusCountdown {
			continue
		}
		out = append(out, a)
	}
	return out
}

// sanityFloor: a wall clock reading before this is a known-bad clock (dead
// RTC boots at some vendor epoch). The value only needs to be earlier than
// any plausible deployment and later than those vendor epochs.
var sanityFloor = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// Evaluate is the scan: one classification pass over the open trips in this
// site's database. It reads persisted state only (plus the in-process
// monotonic receipts, which merely REFINE the persisted wall bridge) — kill
// every tab, restart the process, and the same call reproduces the same
// alerts from disk. Degenerate local trips (§10) are structurally outside
// it: OpenExpectations excludes destination-nil trips and the DeadlineMinutes
// filter below excludes deadline-less ones (chunk row 12).
func (d *Deadlines) Evaluate() (Board, error) {
	exps, err := domain.OpenExpectations(d.st, d.siteID)
	if err != nil {
		return Board{}, fmt.Errorf("deadline: scan: %w", err)
	}
	// evalHook is PANIC-1's test seam (panic1_scan_tick_test.go): it runs on
	// the scanner goroutine after a plain read and immediately BEFORE the
	// scan's first guarded LocalTx (loadCaptures), so a connection held from
	// inside it makes that LocalTx — deterministically the guarded call —
	// wait. Nil in production. ⟦was: the CurrentPhase hook, later in the
	// scan, ahead of recordRedFired — both removed with weather phases,
	// James 2026-10-04⟧
	if d.evalHook != nil {
		d.evalHook()
	}
	caps, err := d.loadCaptures()
	if err != nil {
		return Board{}, err
	}
	// James, 2026-10-03: the ROAD-WORK runs THIS post sent out join the scan
	// (domain/road_work.go: the origin owns them — there is no destination
	// post to own them instead, and the origin's own stream always holds the
	// dispatch, link or no link). Their time basis is the authoring itself:
	// captureOwnAuthored writes the capture row from the log's own
	// authored_at the first time the run is scanned, so `caps` is complete
	// before the loop reads it, and the same classify runs over both kinds.
	// ⟦was: "⚠ READ HERE … and NOT after the phase hook below" — the phase
	// hook PANIC-1 timed against is gone (weather phases removed, James
	// 2026-10-04); PANIC-1's seam is evalHook above⟧
	roadWork, err := domain.OpenRoadWork(d.st, d.siteID)
	if err != nil {
		return Board{}, fmt.Errorf("deadline: scan: %w", err)
	}
	for _, e := range roadWork {
		if e.Trip.DeadlineMinutes == nil {
			continue // dispatchTrip refuses this shape; a replicated foreign payload could still carry it — no limit, no scan
		}
		k := key{origin: e.Trip.OriginSite, epoch: e.Trip.NodeEpoch, seq: e.Trip.Seq}
		if _, have := caps[k]; have {
			continue
		}
		c, ok, err := d.captureOwnAuthored(k, e.Trip.TripID, time.Duration(*e.Trip.DeadlineMinutes)*time.Minute)
		if err != nil {
			return Board{}, err
		}
		if ok {
			caps[k] = c
		}
	}
	// James, 2026-10-03 (NEW-a): the controlled runs THIS post SENT OUT join
	// the scan on the road-work basis — the dispatch is this post's own
	// authored event, so its authored_at IS the recorded instant the clock
	// starts from (captureOwnAuthored: receipt == authoring, G == 0, X == the
	// allowance). The destination keeps its own scan of the same trip on its
	// own clock; the two never compare clocks, and the replicated trip-scoped
	// ack clears both.
	outbound, err := domain.OpenOutbound(d.st, d.siteID)
	if err != nil {
		return Board{}, fmt.Errorf("deadline: scan: %w", err)
	}
	for _, e := range outbound {
		if e.Trip.DeadlineMinutes == nil {
			continue // Dispatch refuses a controlled destination without a deadline; a fixture or foreign payload could still carry it — no allowance, no scan
		}
		k := key{origin: e.Trip.OriginSite, epoch: e.Trip.NodeEpoch, seq: e.Trip.Seq}
		if _, have := caps[k]; have {
			continue
		}
		c, ok, err := d.captureOwnAuthored(k, e.Trip.TripID, time.Duration(*e.Trip.DeadlineMinutes)*time.Minute)
		if err != nil {
			return Board{}, err
		}
		if ok {
			caps[k] = c
		}
	}
	acked, err := ackedTrips(d.st)
	if err != nil {
		return Board{}, err
	}
	d.mu.Lock()
	replaying := d.replaying
	d.mu.Unlock()

	type scanRow struct {
		exp      domain.Expectation
		roadWork bool
		outbound bool
	}
	rows := make([]scanRow, 0, len(exps)+len(roadWork)+len(outbound))
	for _, e := range exps {
		rows = append(rows, scanRow{exp: e})
	}
	for _, e := range roadWork {
		rows = append(rows, scanRow{exp: e, roadWork: true})
	}
	for _, e := range outbound {
		rows = append(rows, scanRow{exp: e, outbound: true})
	}

	var board Board
	open := map[key]bool{}
	for _, row := range rows {
		e := row.exp
		if e.Trip.DeadlineMinutes == nil {
			continue // no deadline, no scan (row 12); nothing is ever written for it
		}
		k := key{origin: e.Trip.OriginSite, epoch: e.Trip.NodeEpoch, seq: e.Trip.Seq}
		open[k] = true
		a := Alert{
			RoadWork: row.roadWork,
			Outbound: row.outbound,
			// ⚠ DR-W7-18 rules THIS surface — the alert strip — to compose
			// the vehicle LIVE rather than read the snapshot, because
			// "VEHICLE OVERDUE: <stale>" misdirects a rescue (W7-36(a)).
			// ⟦James, 2026-10-03⟧ Built now: e.Vehicle is LiveVehicles'
			// composition, the same one the origin's in-transit block reads,
			// so the overdue truck has ONE number at every post.
			// ⟦was: "Vehicle: vehicleSnapshotText(e.Trip.VehicleEntered)"⟧
			TripID: e.Trip.TripID, Vehicle: e.Vehicle,
			Origin: e.Trip.OriginSite, Driver: e.DriverName, Occupants: e.Occupants,
			Deadline: time.Duration(*e.Trip.DeadlineMinutes) * time.Minute,
		}
		if row.outbound && e.Trip.DestinationSite != nil {
			a.Destination = *e.Trip.DestinationSite
		}
		d.classify(&a, k, caps, replaying)
		// R3 (James, 2026-10-04): whether the trip's acks silence this row
		// depends on what the row IS here — an amber ack never silences a
		// red — so Acked is decided after classify, and the faces after it.
		a.Acked = acked.silencesPlain(e.Trip.TripID, a.Status)
		stampAckFaces(&a, acked.tripFaces[e.Trip.TripID])

		board.Alerts = append(board.Alerts, a)

		// ⟦was: Ruling F39 (the deadline_red_fired marker and the
		// post-rescale "was OVERDUE before the phase change" row) and §7 (the
		// per-trip weather VERIFY row) — both removed with weather phases,
		// James 2026-10-04. Nothing rescales an allowance any more, so a
		// fired red can no longer return to a countdown by arithmetic.⟧
	}

	// Housekeeping: monotonic receipts for trips no longer open are dead
	// weight (C6 left the same note on its render-time map). Purely an
	// in-memory prune; capture rows on disk are never deleted.
	d.mu.Lock()
	for k := range d.receipts {
		if !open[k] {
			delete(d.receipts, k)
		}
	}
	d.mu.Unlock()

	return board, nil
}

// classify applies §9.3/§9.4 to one open expectation. The order of the cases
// is the safety argument: every branch that cannot PROVE a live countdown is
// still credible lands in overdue-unresolved — erring early, never late.
//
// ⟦was: "Weather phases change exactly one input: the ALLOWANCE (X),
// recomputed every scan under phaseNow (§5.1) …" — weather phases removed,
// James 2026-10-04; X is the officer's authored allowance, unscaled⟧
func (d *Deadlines) classify(a *Alert, k key, caps map[key]capture, replaying bool) {
	// 1. Replay gate (§9.4): a from-zero replay is re-delivering history; no
	// receipt time minted during it means anything yet.
	if replaying {
		a.Status = StatusOverdueUnresolved
		a.Reason = "from-zero replay in progress — every open expectation needs verification by radio (§9.4)"
		a.DeadlineUnknown = true
		return
	}

	c, ok := caps[k]
	// 2. No persisted capture: this box has no receipt record for the
	// expectation, so no countdown can be credible (ruling 3 — never a live
	// countdown from the current anchor; the store records no receipt time
	// that could retroactively justify one).
	if !ok {
		a.Status = StatusOverdueUnresolved
		a.Reason = "no receipt record exists for this expectation — verify by radio"
		a.DeadlineUnknown = true
		return
	}

	// 3. Captured without an anchor (restart/replay catch-up): G unknown,
	// X−G unknowable — review, never a guess.
	//
	// Q-FE2 (James, 2026-10-04): the row stays AMBER while the RECORDED due
	// time (DueAt — the run's effective departure plus X; James 2026-10-06:
	// from the typed time) is in the future, and
	// turns into the live red when it passes, so both ends of the run raise
	// the same red. ⟦was: amber "OVERDUE — UNRESOLVED" for the whole life of
	// the trip, returned before any time test — FINDINGS-R5 A6⟧ Everything
	// it reads is persisted (the capture, its witness, the dispatch's
	// authored_at), so the state survives a restart; nothing new is stored.
	if !c.anchorOK || !c.remainingMs.Valid {
		a.Status = StatusOverdueUnresolved
		a.Reason = "received before this box had a sync anchor (restart or replay catch-up) — link-down time at receipt is unknown; verify by radio"
		a.DeadlineUnknown = true
		due, ok := d.recordedDue(k, a.Deadline)
		if !ok {
			return // the dispatch is not in this store: no due time on record, amber
		}
		a.DueAt = due
		if now, ok := d.nowAgainstDue(c); ok && now.After(due) {
			d.raiseRed(a, now.Sub(due))
		}
		return
	}

	remainingAtReceipt := time.Duration(c.remainingMs.Int64) * time.Millisecond

	// 4. X−G ≤ 0 at receipt: the deadline passed DURING the partition. The
	// §9.3 catch-up rule — review lane with the link-down framing, never a
	// live alert, never a fresh countdown, no post-heal storm.
	if remainingAtReceipt <= 0 {
		downMin := int64(0)
		if c.gMs.Valid {
			downMin = (c.gMs.Int64 + 59_999) / 60_000 // whole minutes, rounded up
		}
		a.Status = StatusOverdueUnresolved
		a.Reason = fmt.Sprintf("link was down %d min — deadline passed before this site could hear the dispatch; verify by radio (§9.3)", downMin)
		// Shown and used to order the strip (Q-FE2); this row's verdict is
		// unchanged — §9.3 never turns a partition-expired run red.
		a.DueAt, _ = d.recordedDue(k, a.Deadline)
		return
	}

	// 5. A credible countdown exists: remaining = X − G − lead − elapsed.
	// The lead is how far the dispatch's typed time precedes its recording
	// (James 2026-10-06: from the typed time — departureLead), both on the
	// origin's clock, so a run typed back past its allowance is OVERDUE at
	// once. The stored remaining_ms served only branch 4's frozen
	// receipt-time verdict (the partition framing: a lead alone never puts
	// a run in the review lane). ⟦was: X_eff(phase_now), the weather-phase rescale and its
	// PhaseFace — removed with weather phases, James 2026-10-04⟧
	xEff := a.Deadline
	g := time.Duration(c.gMs.Int64) * time.Millisecond

	d.mu.Lock()
	receipt, live := d.receipts[k]
	d.mu.Unlock()
	var elapsed time.Duration
	if live {
		elapsed = time.Since(receipt) // monotonic — immune to wall-clock steps
	} else {
		wallNow := d.WallNow()
		receiptWall := time.UnixMilli(c.receiptWallMs)
		if wallNow.Before(sanityFloor) {
			a.Status = StatusOverdueUnresolved
			a.Reason = "this box's wall clock is implausible after a restart — elapsed time is unknowable; verify by radio"
			a.DeadlineUnknown, a.ClockChanged = true, true
			d.judgeOnBootClock(a, k, c)
			return
		}
		if wallNow.Before(receiptWall) {
			// Backwards clock across a restart: elapsed is unknowable and
			// max(0, ·) would systematically UNDER-count — the §9.3 hole by
			// another door. Review lane (the chunk's rider pins this branch).
			a.Status = StatusOverdueUnresolved
			a.Reason = "wall clock went backwards across a restart — elapsed time is unknowable; verify by radio"
			a.DeadlineUnknown, a.ClockChanged = true, true
			d.judgeOnBootClock(a, k, c)
			return
		}
		// R9 (James, 2026-10-04): the FORWARD step, and a backward one
		// smaller than the whole span since receipt — the two the check
		// above cannot see. ⟦was: elapsed = wallNow.Sub(receiptWall),
		// unchecked: a +2 h step read as 2 h of travel⟧
		var changed string
		if elapsed, changed = d.bridgeElapsed(c, wallNow); changed != "" {
			a.Status = StatusOverdueUnresolved
			a.Reason = changed
			a.DeadlineUnknown, a.ClockChanged = true, true
			d.judgeOnBootClock(a, k, c)
			return
		}
	}

	if remaining := xEff - g - d.departureLead(k) - elapsed; remaining > 0 {
		a.Status = StatusCountdown
		a.Remaining = remaining
		return
	} else {
		d.raiseRed(a, -remaining)
	}
}

// raiseRed makes a the live red, overdue by `by`, with the staleness face —
// the one way every branch reaches StatusOverdueLive, so a Q-FE2 red (from
// the recorded due time or the boot clock) is the same red as a countdown
// that ran out: same strip, banner, board, logged ack and R3 kind.
func (d *Deadlines) raiseRed(a *Alert, by time.Duration) {
	a.Status = StatusOverdueLive
	a.OverdueBy = by
	a.Reason, a.DeadlineUnknown = "", false

	// James, 2026-10-03: an own-authored road-work run has no link to be
	// stale over — the origin IS this post, and "last heard from <ourselves>:
	// never" is a true sentence about the engine that misreads as a fault.
	// No face; the sync chip still reports the peers.
	if a.RoadWork {
		return
	}
	// §9.3 rule 3 (A1d): a live alert raised while stale carries the
	// staleness on its face. Unknown staleness reads as stale — the safe
	// direction. The post named is the FAR end of the run (farPost): for an
	// outbound row that is the destination, whose unheard arrival record is
	// exactly what would make this red a false one.
	//
	// FIX5 (R7 L01/L05, 2026-10-05): the face is its own sentence on every
	// surface (banner.html renders it after the row's full stop), so it
	// opens with a capital, and it names the post by the display name the
	// rest of the row uses (sitelabel), never the raw id. Staleness is still
	// looked up by id. ⟦was: "not heard from ice-pad since this post started
	// …", "last heard from ice-pad: 4 minutes ago"⟧
	far := a.farPost()
	name := d.siteName(far)
	if d.Staleness == nil {
		a.StalenessFace = fmt.Sprintf("Last heard from %s: unknown", name)
		return
	}
	if ago, ok := d.Staleness(far); !ok {
		a.StalenessFace = fmt.Sprintf("Last heard from %s: never", name)
		if d.ProcessUptime != nil {
			a.StalenessFace = fmt.Sprintf("Not heard from %s since this post started %s", name, humantime.Ago(d.ProcessUptime()))
		}
	} else if ago > d.syncInterval {
		// Shared phrasing (internal/humantime) with C10's sync-status ages —
		// was raw Duration.String() ("4m32s"), now "4 minutes ago" to match
		// the design's own §8.9 example; see humantime's doc comment.
		a.StalenessFace = fmt.Sprintf("Last heard from %s: %s", name, humantime.Ago(ago))
	}
}

// siteName renders a site id the way this post's pages name it: the store's
// SiteNamer (installed by the web layer from config — the same function as
// the templates' sitelabel), else domain.SiteLabel.
func (d *Deadlines) siteName(id string) string {
	if d.st != nil && d.st.SiteNamer != nil {
		return d.st.SiteNamer(id)
	}
	return domain.SiteLabel(id)
}

// Run is the §9.4 periodic scan loop: evaluate every sync interval, log
// status transitions loudly (the console is an operator surface on these
// boxes). All alert STATE lives in the database — this loop holds only a
// cosmetic transition memory, and every page render re-runs Evaluate anyway,
// so a dead ticker degrades visibility, never truth.
func (d *Deadlines) Run(ctx context.Context) {
	period := d.syncInterval
	if period <= 0 {
		period = 10 * time.Second
	}
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(period)
		}
	}
}

// tick is one scan of the loop above, and the loop's whole contract with
// the process: LOUD, NEVER FATAL. ⟦PANIC-1, 2026-09-30⟧ Evaluate reads the
// store through LocalTx (the amendment composition under OpenExpectations,
// the captures), and the store's reentrancy guard panics
// when it judges a wait for the single connection to be reentrancy. On
// this goroutine nothing recovered that panic, so it was fatal: MEASURED
// (L07 round 1; L11 round 2, ice-pad and ndb in the same second) a post
// died mid-scan and was dark at the gate until systemd restarted it. The
// guard no longer panics on mere contention (store/reentrancy_guard.go),
// and whatever it or anything under Evaluate still panics on is contained
// HERE: logged with its stack — a programming error stays a programming
// error, in the log where an operator reads — and the tick is skipped.
// Skipping is safe by the loop's own doc comment: all alert STATE lives in
// the database, every page render re-runs Evaluate, and the next tick runs
// on schedule.
func (d *Deadlines) tick(period time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("deadline: SCAN TICK PANICKED — this tick is skipped, the next runs in %s; the post stays up. panic: %v\n%s",
				period, r, debug.Stack())
		}
	}()
	board, err := d.Evaluate()
	if err != nil {
		log.Printf("deadline: scan failed: %v", err)
		return
	}
	d.logTransitions(board)
}

func (d *Deadlines) logTransitions(board Board) {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := map[string]bool{}
	for _, a := range board.Alerts {
		seen[a.TripID] = true
		if d.lastLog[a.TripID] != a.Status {
			d.lastLog[a.TripID] = a.Status
			switch a.Status {
			case StatusOverdueLive:
				log.Printf("deadline: ALERT vehicle %q %s is OVERDUE by %s (trip %s)%s",
					a.Vehicle, a.runText(), a.OverdueBy.Round(time.Second), a.TripID, suffixFace(a))
			case StatusOverdueUnresolved:
				log.Printf("deadline: REVIEW vehicle %q %s is overdue-unresolved (trip %s): %s",
					a.Vehicle, a.runText(), a.TripID, a.Reason)
			case StatusCountdown:
				log.Printf("deadline: counting down trip %s (%s remaining)", a.TripID, a.Remaining.Round(time.Second))
			}
		}
	}
	for k := range d.lastLog {
		if !seen[k] {
			delete(d.lastLog, k)
		}
	}
}

// runText names the run's direction for the operator log: "from <origin>"
// for an inbound row, "OUTBOUND to <destination>" for one this post sent out
// (NEW-a), "working on the road" for equipment.
func (a Alert) runText() string {
	switch {
	case a.RoadWork:
		return "working on the road from " + a.Origin
	case a.Outbound:
		return "OUTBOUND from " + a.Origin + " to " + a.Destination
	default:
		return "from " + a.Origin
	}
}

func suffixFace(a Alert) string {
	if a.StalenessFace == "" {
		return ""
	}
	return " — " + a.StalenessFace
}

// --- template-facing text helpers (presentation of durations; the numeric
// fields stay the source of truth for tests and logs) ---

// OverdueByText renders OverdueBy for the banner.
func (a Alert) OverdueByText() string { return a.OverdueBy.Round(time.Second).String() }

// RemainingText renders Remaining for the board.
func (a Alert) RemainingText() string { return a.Remaining.Round(time.Second).String() }

// vehicleSnapshotText reads the frozen dispatch-time number as display text.
// ⚠ It returns the EMPTY STRING for an absent number rather than any stand-in
// wording: the empty string IS the absence (DR-W7-15), and a sentinel here
// would be a magic string in the vehicle field — which [[D-11]] rules out in
// terms. What an operator should SEE for a numberless truck is W7-26's shared
// render helper's business, not this scan's.
func vehicleSnapshotText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
