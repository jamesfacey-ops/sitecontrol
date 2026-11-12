// clockstep.go — R9 (FINDINGS-R4 D5): the wall bridge's forward-step check.
//
// James, 2026-10-04: detect a FORWARD clock step the same way as a backward
// one — compare the persisted receipt wall time plus uptime with the new
// clock — and beyond a tolerance send the trip to "clock changed since
// receipt — verify by radio", never a fabricated OVERDUE.
//
// The hole it closes (MEASURED, L16-SKa, re-run on L05 2026-10-04): a post
// restarted with its wall clock stepped +2 h read a trip with 65 min left as
// "OVERDUE 55m16s" while the origin still read "DUE IN 1h4m52s". The bridge
// across a restart is wallNow − receipt_wall_ms; a clock that went
// BACKWARDS is refused (wallNow < receipt), but a clock that jumped FORWARD
// looked exactly like time passing. The same bridge runs on every scan, with
// no restart at all, for an own-authored run (road work, the origin's
// outbound row), so a step while running moved those rows too.
//
// The second clock is the host's CLOCK_BOOTTIME: monotonic, counts suspend,
// and never stepped by settimeofday or an NTP step. At capture this records
// a WITNESS — (wall, boot clock, boot id) read together — and every later
// bridge compares how far each clock has moved since:
//
//	same boot      the two deltas must agree within clockStepTolerance; when
//	               they do, elapsed is taken from the BOOT clock (exact, so a
//	               sub-tolerance step moves nothing either); when they do not,
//	               the clock changed — review lane, never a countdown and
//	               never a live red.
//	host rebooted  the boot clock restarted, so only one direction is
//	               provable: the box cannot have been up LONGER than the
//	               wall says has passed since the witness. Up longer ⇒ the
//	               clock went back ⇒ review. A forward step across a host
//	               reboot is indistinguishable from the box having been off
//	               that long — stated, not hidden (the residual the report
//	               names). James, 2026-10-04 (Q-FE1): accepted as a
//	               DOCUMENTED LIMIT — a forward step across a HOST reboot is
//	               undetectable here, and the bridge then reads the wall as
//	               before R9. It is a host reboot only: inside an LXC
//	               (CT510) boot_id and CLOCK_BOOTTIME are the HOST's, so a
//	               container or process restart is the same boot and a step
//	               across it is caught by the same-boot arm above
//	               (FINDINGS-R5 §E). Q-FE2's red on a clock-changed row
//	               (due.go) is likewise judged only on the same boot; across
//	               a host reboot that row stays VERIFY BY RADIO.
//	no witness     a capture written before this check existed, or a host
//	               where the boot clock is unreadable: the older checks
//	               (implausible, backwards) only, exactly as before.
//
// The witness table is site-local and separate from deadline_capture so an
// existing post's database needs no column migration: CREATE TABLE IF NOT
// EXISTS is the whole upgrade, and an old row simply has no witness.
package deadline

import (
	"database/sql"
	"fmt"
	"time"

	"sitecontrol/internal/humantime"
)

// clockStepTolerance: how far the wall and boot clocks may disagree since a
// witness before the bridge is refused. Both clocks are disciplined by the
// same NTP frequency correction, so honest drift between them is
// milliseconds; the tolerance only has to absorb an NTP STEP small enough
// not to matter and the read gap between the two clocks. 30 s is three
// routine sync intervals and half the smallest allowance the dispatch form
// takes (1 min) — far below any step that would move a countdown by a
// figure an officer acts on, and on the same-boot path a sub-tolerance step
// moves nothing anyway (elapsed comes from the boot clock there).
const clockStepTolerance = 30 * time.Second

const clockWitnessSchema = `
CREATE TABLE IF NOT EXISTS deadline_clock_witness (
    origin_site TEXT    NOT NULL,
    node_epoch  INTEGER NOT NULL,
    seq         INTEGER NOT NULL,
    wall_ms     INTEGER NOT NULL,
    up_ms       INTEGER NOT NULL,
    boot_id     TEXT    NOT NULL,
    PRIMARY KEY (origin_site, node_epoch, seq)
) STRICT;`

// clockWitness is one (wall, boot clock, boot id) reading taken together.
type clockWitness struct {
	wallMs int64
	upMs   int64
	boot   string
}

// HostUptime is the production second clock (main wires Deadlines.Uptime to
// it): the kernel's boot id and CLOCK_BOOTTIME on linux, ok=false elsewhere.
func HostUptime() (bootID string, up time.Duration, ok bool) { return hostUptime() }

// readWitness takes a witness now; ok=false when there is no second clock.
func (d *Deadlines) readWitness(wall time.Time) (clockWitness, bool) {
	if d.Uptime == nil {
		return clockWitness{}, false
	}
	boot, up, ok := d.Uptime()
	if !ok {
		return clockWitness{}, false
	}
	return clockWitness{wallMs: wall.UnixMilli(), upMs: int64(up / time.Millisecond), boot: boot}, true
}

// writeWitness persists a witness for one capture, write-once like the
// capture itself (INSERT OR IGNORE on the source key).
func writeWitness(tx *sql.Tx, k key, w clockWitness) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO deadline_clock_witness (origin_site, node_epoch, seq, wall_ms, up_ms, boot_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		k.origin, k.epoch, k.seq, w.wallMs, w.upMs, w.boot)
	return err
}

// bridgeElapsed is the wall bridge with the R9 check: how long since the
// receipt instant, by this box, when the in-process monotonic receipt is
// gone (a restart) or never existed (an own-authored run). reason != ""
// means the clock changed since receipt and no elapsed figure is honest.
func (d *Deadlines) bridgeElapsed(c capture, wallNow time.Time) (elapsed time.Duration, reason string) {
	receiptWall := time.UnixMilli(c.receiptWallMs)
	elapsed = wallNow.Sub(receiptWall)
	if c.witness == nil {
		return elapsed, ""
	}
	now, ok := d.readWitness(wallNow)
	if !ok {
		return elapsed, ""
	}
	w := *c.witness
	wallDelta := time.Duration(now.wallMs-w.wallMs) * time.Millisecond
	if now.boot == w.boot {
		upDelta := time.Duration(now.upMs-w.upMs) * time.Millisecond
		if step := wallDelta - upDelta; step > clockStepTolerance || step < -clockStepTolerance {
			return 0, clockChangedReason(step)
		}
		// The boot clock is the exact figure since the witness; the wall
		// only spans receipt → witness (zero for an inbound capture, the
		// first scan's lag for an own-authored one).
		return time.Duration(w.wallMs-c.receiptWallMs)*time.Millisecond + upDelta, ""
	}
	// Host rebooted: up since boot cannot exceed wall time since the witness.
	if up := time.Duration(now.upMs) * time.Millisecond; up > wallDelta+clockStepTolerance {
		return 0, clockChangedReason(wallDelta - up)
	}
	return elapsed, ""
}

// bootElapsed is the time since the receipt instant by the BOOT clock alone
// (Q-FE2's judge for a clock-changed row, due.go): the witness's own wall
// span from receipt (zero for an inbound capture, one scan's lag for an
// own-authored one) plus CLOCK_BOOTTIME since the witness. ok=false unless
// the capture has a witness and this post is still on the witness's boot —
// across a host reboot, or with no witness, there is no honest figure.
func (d *Deadlines) bootElapsed(c capture) (time.Duration, bool) {
	if c.witness == nil {
		return 0, false
	}
	now, ok := d.readWitness(d.WallNow())
	if !ok || now.boot != c.witness.boot {
		return 0, false
	}
	w := *c.witness
	return time.Duration(w.wallMs-c.receiptWallMs)*time.Millisecond +
		time.Duration(now.upMs-w.upMs)*time.Millisecond, true
}

// clockChangedReason is the review-lane sentence; step > 0 is forward.
func clockChangedReason(step time.Duration) string {
	dir := "forward"
	if step < 0 {
		dir, step = "back", -step
	}
	// humantime.Ago's units without its "ago": "2 hours", "45 minutes" —
	// ROUNDED to the unit first (FINDINGS-R5 F8). Span truncates, which is
	// right for an age but wrong for a measured step: +2 h read back as
	// 1 h 59 m 5x s and printed "about 1 hour" on ~15 % of renders.
	// ⟦was: humantime.Span(step)⟧ Rounded here, not in shared humantime,
	// whose other callers rely on truncation (an age never rounds up).
	by := humantime.Span(roundToSpanUnit(step))
	return fmt.Sprintf("clock changed since receipt — this box's wall clock moved %s by about %s against its own uptime, so elapsed time is unknowable; verify by radio", dir, by)
}

// roundToSpanUnit rounds d to the unit humantime.Span will print it in —
// seconds under a minute, minutes under an hour, hours under a day, else
// days — so Span's truncation then prints the NEAREST figure. Rounding can
// carry into the next unit (59 m 40 s → 60 m → "1 hour"), which is correct.
func roundToSpanUnit(d time.Duration) time.Duration {
	switch {
	case d < time.Minute:
		return d.Round(time.Second)
	case d < time.Hour:
		return d.Round(time.Minute)
	case d < 24*time.Hour:
		return d.Round(time.Hour)
	default:
		return d.Round(24 * time.Hour)
	}
}

// The R9 faces, one spelling for every surface that renders a ClockChanged
// row (the trips board, the inbound list, the road-work list): the row says
// what is known — this box's clock moved — and never OVERDUE, which would be
// the fabricated verdict R9 forbids.
const (
	ClockChangedLabel = "CLOCK CHANGED — VERIFY BY RADIO"
	ClockChangedNote  = "clock changed since receipt — verify by radio"
)
