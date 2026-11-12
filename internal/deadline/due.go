// due.go — Q-FE2 (James, 2026-10-04: "no-anchor/clock rows stay AMBER,
// relabel VERIFY BY RADIO, show recorded due time, turn RED when that due
// time passes").
//
// FINDINGS-R5 A6 measured the hole: a run received before the destination
// had a sync anchor (any run in flight across a destination restart) read
// amber "OVERDUE — UNRESOLVED" there for the whole life of the trip —
// classify's branch 3 returned before any time test — while the origin
// counted down and raised the only red. The same was true of an R9
// clock-changed row. Two judges now turn those rows red, and neither is a
// guess:
//
//	no sync anchor   the RECORDED due time: the run's effective departure
//	                 (domain.EffectiveDeparture — the dispatch's typed time,
//	                 James 2026-10-06: "from the typed time"; else its
//	                 authored_at, the origin's clock) plus the allowance —
//	                 the basis third-post pages show. It is compared with this post's "now", read off
//	                 the boot clock when the capture's witness is on the
//	                 same boot (a wall step since receipt then moves
//	                 nothing), else the wall clock, refused when it is
//	                 implausible or behind the receipt.
//	clock changed    the BOOT clock only (lead, 2026-10-04): CLOCK_BOOTTIME
//	                 elapsed since receipt — R9's witness already records it
//	                 — against the allowance left at receipt (X − G), and
//	                 only while the post is on the receipt's boot. With no
//	                 same-boot witness (a host reboot, a legacy capture with
//	                 no witness) the row stays amber VERIFY BY RADIO for
//	                 good: it never turns red on wall-clock arithmetic,
//	                 because the wall is exactly the clock that changed.
//
// Everything read is persisted — the capture row, its witness, the event
// log's authored_at — so the verdict survives a restart and no table is
// added. A pre-due amber ack does not silence the later red: R3's
// silencesPlain reads the row's CURRENT status, and an amber ack never
// silences a red.
package deadline

import (
	"log"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// recordedDue is the run's recorded due instant: the dispatch's effective
// departure (James 2026-10-06: "from the typed time" — domain.
// EffectiveDeparture; ⟦was: FINDINGS-R4 D13, the envelope's authored_at⟧)
// plus the allowance. ok=false when this store does not hold the dispatch (a
// from-zero rebuild mid-replay) or the read fails — logged, and the row
// stays amber, the safe reading of "no due time on record".
func (d *Deadlines) recordedDue(k key, x time.Duration) (time.Time, bool) {
	dep, _, found, err := domain.DispatchDeparture(d.st, store.Key{SiteID: k.origin, NodeEpoch: k.epoch, Seq: k.seq})
	if err != nil {
		log.Printf("deadline: recorded due time for %v unreadable: %v — the row stays VERIFY BY RADIO", k, err)
		return time.Time{}, false
	}
	if !found {
		return time.Time{}, false
	}
	return dep.Add(x), true
}

// departureLead is how far the dispatch's typed time precedes its recording
// (domain.DepartureLead; James 2026-10-06: "from the typed time"), taken off
// every countdown this scan runs. 0 when the dispatch is not in this store
// or unreadable (logged): the recorded-time basis, as before the ruling.
func (d *Deadlines) departureLead(k key) time.Duration {
	_, lead, _, err := domain.DispatchDeparture(d.st, store.Key{SiteID: k.origin, NodeEpoch: k.epoch, Seq: k.seq})
	if err != nil {
		log.Printf("deadline: typed departure for %v unreadable: %v — counting from the recorded time", k, err)
	}
	return lead
}

// nowAgainstDue is this post's "now" for the comparison with a recorded due
// time. On the receipt's boot it is the witness's wall reading carried
// forward on the boot clock, so a wall step since receipt cannot move the
// verdict either way; otherwise it is the wall clock, refused (ok=false —
// the row stays amber) when it is implausible or behind the receipt.
func (d *Deadlines) nowAgainstDue(c capture) (time.Time, bool) {
	wallNow := d.WallNow()
	if c.witness != nil {
		if now, ok := d.readWitness(wallNow); ok && now.boot == c.witness.boot {
			up := time.Duration(now.upMs-c.witness.upMs) * time.Millisecond
			return time.UnixMilli(c.witness.wallMs).Add(up), true
		}
	}
	if wallNow.Before(sanityFloor) || wallNow.Before(time.UnixMilli(c.receiptWallMs)) {
		return time.Time{}, false
	}
	return wallNow, true
}

// judgeOnBootClock runs on an R9 clock-changed row (already amber, Reason
// set): it records the due time for display and ordering, and raises the
// red once the boot-clock elapsed since receipt has used up the allowance
// left at receipt. Without a same-boot witness it changes nothing — amber
// for good.
func (d *Deadlines) judgeOnBootClock(a *Alert, k key, c capture) {
	a.DueAt, _ = d.recordedDue(k, a.Deadline)
	elapsed, ok := d.bootElapsed(c)
	if !ok || !c.gMs.Valid {
		return
	}
	g := time.Duration(c.gMs.Int64) * time.Millisecond
	if remaining := a.Deadline - g - d.departureLead(k) - elapsed; remaining <= 0 {
		d.raiseRed(a, -remaining)
	}
}
