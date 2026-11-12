// Package deadline is C7: the server-side deadline scanner and alert surface
// (design §9.3/§9.4). It owns
//
//   - the CAPTURE: on every applied envelope (C3a's Engine.OnApplied hook,
//     synchronous, pre-rotation anchor) it computes G and persists
//     (receipt wall-time, remaining-at-receipt = X−G) for inbound deadline
//     trips — the §9.3 rule, pinned at receipt, because the anchor's currency
//     is drain-relative and polling Anchor() later is structurally wrong
//     (C3a review ruling 3 / carry-forward rows 15 and 18);
//   - the SCAN: a periodic server-side evaluation of open trips in this
//     site's database (§9.4) — alert state is data, not a UI event; nothing
//     alert-critical lives in process memory or a browser tab;
//   - the REBOOT BRIDGE: in-process, elapsed time runs on the monotonic
//     clock; across a process/host restart it is bridged by the persisted
//     receipt wall-time, and a backwards or known-bad clock drops the trip to
//     the overdue-unresolved review lane (erring early, §9.3) — as does,
//     since R9 (James, 2026-10-04), a clock STEPPED either way against the
//     host's boot clock (clockstep.go);
//   - the REPLAY GATE: while a from-zero replay (§8.6 rejoin) is in progress
//     every open trip EXPECTATION is overdue-unresolved, never live;
//     degenerate local trips carry no deadline and are untouched;
//   - the ACKNOWLEDGMENT: an explicit, logged event (ack.go) that dismisses
//     the banner (§9.4/A14).
//
// Capture state lives in a SITE-LOCAL table (store.RegisterLocalSchema /
// LocalTx): it records B-local receipt instants that exist only at the
// destination site, so it must never appear in the cross-site projection
// dump. The deadline ARITHMETIC stays clock-free in the design's sense: it
// runs on this box's monotonic clock from receipt; the persisted wall-time is
// only the bridge across a restart and is never compared across sites.
package deadline

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// key is a trip event's source key — origin_site plus (node_epoch, seq).
// C7 state is keyed by this, NEVER by a trip_state row id: trip_state is
// DELETE + re-INSERT wholesale on every trip-family event (carry-forward
// row 30), so row identity there ceases to exist while the vehicle is still
// on the ice. (node_epoch, seq) alone orders only within one site's stream
// (§8.3), hence origin_site completes the key.
type key struct {
	origin string
	epoch  int64
	seq    int64
}

// captureSchema is the C7-owned local DDL (convention: the DDL text lives in
// the owning package; store only executes it — same shape as
// Projection.Schema, minus the dump).
//
// receipt_wall_ms is the bridge's wall-clock value, and it exists solely as
// the reboot bridge (§9.4): a monotonic instant is meaningless across a
// restart. (R9 adds one more, deadline_clock_witness.wall_ms, read beside the
// boot clock only to CHECK that bridge — clockstep.go; neither is ever
// compared across sites.) remaining_ms/g_ms/x_receipt_ms are NULL when the receipt
// had no §9.3 anchor (no full drain since process start —
// restart or from-zero replay): G is then unknown and the trip can only ever
// be overdue-unresolved, never a live countdown.
//
// x_receipt_ms is the authored allowance X as captured at receipt; together
// with g_ms it fixes the CREDIBILITY verdict once, at receipt (remaining_ms =
// x_receipt − G, ≤ 0 ⇒ review lane, sticky both ways). ⟦was: "the effective
// X under the phase this site knew AT receipt … the countdown itself is
// recomputed on every scan as X_eff(phase_now) − G − elapsed" — weather
// phases removed, James 2026-10-04; X is never rescaled now⟧
const captureSchema = `
CREATE TABLE IF NOT EXISTS deadline_capture (
    origin_site     TEXT    NOT NULL,
    node_epoch      INTEGER NOT NULL,
    seq             INTEGER NOT NULL,
    trip_id         TEXT    NOT NULL,
    receipt_wall_ms INTEGER NOT NULL,
    remaining_ms    INTEGER,
    g_ms            INTEGER,
    x_receipt_ms    INTEGER,
    anchor_ok       INTEGER NOT NULL CHECK (anchor_ok IN (0, 1)),
    PRIMARY KEY (origin_site, node_epoch, seq)
) STRICT;`

// ⟦was: redFiredSchema — the site-local deadline_red_fired table (operator
// ruling F39), which existed only to word the "was OVERDUE before the phase
// change" row after a weather-phase rescale returned a fired red to a
// countdown. Removed with weather phases, James 2026-10-04: nothing rescales
// an allowance now. A developer database still holding the table keeps it,
// unread.⟧

// Deadlines is one site's C7 machinery. Construct with New, hand OnApplied to
// the replication engine, run Run in a goroutine, render Evaluate.
type Deadlines struct {
	st           *store.Store
	siteID       string
	syncInterval time.Duration

	// Staleness reports current staleness to an origin site ("how long since
	// we last heard from it"); ok=false means never/unknown. Wired from the
	// engine's snapshot in main; nil-safe (treated as unknown — the alert
	// then carries staleness on its face, the early-erring direction).
	Staleness func(origin string) (time.Duration, bool)

	// ProcessUptime reports how long this post's process has run (the engine's
	// Snapshot.UptimeSec). Not the R9 boot clock (Uptime, below): this is
	// process age, used only to word the staleness face. Staleness is in-memory, so its "never" means
	// "not since this process started": with ProcessUptime wired the face says so
	// ("not heard from X since this post started N ago") instead of "never",
	// which after a restart read as "this post has never heard from X" for a
	// whole partition (sc-test L08 T-074, FIX2 cluster K). nil-safe: nil
	// keeps the old "never".
	ProcessUptime func() time.Duration

	// WallNow is the wall clock, injectable in tests (the reboot-bridge and
	// backwards-clock branches are otherwise untestable without a real
	// reboot). Monotonic arithmetic always uses the runtime's own reading —
	// tests cannot and must not fake that.
	WallNow func() time.Time

	// Uptime is the R9 second clock (clockstep.go): the host's boot id and
	// time since boot, read together with WallNow into a witness at every
	// capture. Wired in main to HostUptime (CLOCK_BOOTTIME), like Staleness;
	// nil-safe — nil, or ok=false, means no
	// second clock and the wall bridge keeps only its older checks. Left
	// nil by New on purpose: a test that fakes WallNow to make time PASS
	// would otherwise read as a clock step against the real boot clock;
	// the R9 tests inject both clocks together.
	Uptime func() (bootID string, up time.Duration, ok bool)

	// ⟦was: CurrentPhase, TravelMinutes, ReviewInTransit and
	// PhaseDisplayName — the weather-phase funcs wired in main; removed with
	// weather phases, James 2026-10-04⟧

	// evalHook: PANIC-1's test seam — see its call in Evaluate. Nil in
	// production; set only by panic1_scan_tick_test.go.
	evalHook func()

	mu sync.Mutex
	// receipts holds the in-process MONOTONIC receipt instant per captured
	// trip. While the process lives, elapsed = time.Since(receipt) — immune
	// to any wall-clock change. Lost on restart BY DESIGN; the persisted
	// receipt_wall_ms bridges that, and the two failure modes of the bridge
	// (backwards / known-bad clock) drop to the review lane. Never
	// alert-critical on its own: every entry has a persisted capture row.
	receipts  map[key]time.Time
	replaying bool
	lastLog   map[string]Status // Run()'s transition log memory, by trip id; cosmetic only
}

// New prepares the C7 machinery: local schema registered, no goroutines yet.
func New(st *store.Store, siteID string, syncInterval time.Duration) (*Deadlines, error) {
	if err := st.RegisterLocalSchema("deadline (C7)", captureSchema); err != nil {
		return nil, err
	}
	if err := st.RegisterLocalSchema("deadline (C7, clock witness R9)", clockWitnessSchema); err != nil {
		return nil, err
	}
	return &Deadlines{
		st: st, siteID: siteID, syncInterval: syncInterval,
		WallNow:  time.Now,
		receipts: make(map[key]time.Time),
		lastLog:  make(map[string]Status),
	}, nil
}

// SetReplaying raises/lowers the §9.4 replay gate. main.go raises it around
// engine.Rejoin (the §8.6 from-zero replay path); while raised, Evaluate
// classifies every open trip expectation overdue-unresolved.
func (d *Deadlines) SetReplaying(v bool) {
	d.mu.Lock()
	d.replaying = v
	d.mu.Unlock()
}

// OnApplied is the capture — wired as the engine's applied-envelope hook. It
// fires synchronously per APPLIED envelope on the pull goroutine, with the
// pre-rotation §9.3 anchor (rotation happens only at cycle end, so the
// in-loop read is structurally pre-rotation). This is the ONLY place a
// capture row is ever written: the scan never invents receipt times, so an
// open inbound trip first sighted with no persisted capture can only be
// overdue-unresolved — never a live countdown from the current Anchor()
// (ruling 3), whose currency the last rotation may already have destroyed
// for this event (row 18).
//
// INSERT OR IGNORE on the source-key PK: a later state change can NEVER
// re-anchor. C6's fold can legitimately REOPEN a bound trip when a second
// matching trip syncs in (row 29); no new trip_dispatched envelope exists at
// that moment, so this hook does not even fire — and if the same envelope
// were somehow re-delivered, the idempotent apply loop drops it before the
// hook. G is captured once, at first receipt, or it is unknown.
func (d *Deadlines) OnApplied(env store.Envelope, anchor time.Time, anchorOK bool) {
	var p domain.TripDispatched
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return // store guarantees valid JSON; a non-object payload is not ours
	}
	// Only inbound deadline expectations are captured: degenerate local trips
	// (both nil, §10) and trips destined elsewhere never enter C7 state.
	if p.Type != "trip_dispatched" || p.DeadlineMinutes == nil ||
		p.DestinationSite == nil || *p.DestinationSite != d.siteID {
		return
	}
	receipt := time.Now() // carries the monotonic reading
	k := key{origin: env.SiteID, epoch: env.NodeEpoch, seq: env.Seq}
	x := time.Duration(*p.DeadlineMinutes) * time.Minute

	var remainingMs, gMs, xReceiptMs any // NULL when the anchor is absent
	anchorOKInt := 0
	if anchorOK {
		g := receipt.Sub(anchor)
		if g < 0 {
			g = 0 // anchor is always in the past; guard against a misuse
		}
		// The credibility verdict's X: the officer's authored deadline,
		// judged once, at receipt. ⟦was: scaled to the weather phase this
		// site knew at receipt — removed with weather phases, James
		// 2026-10-04⟧
		xReceipt := x
		remainingMs = int64((xReceipt - g) / time.Millisecond)
		gMs = int64(g / time.Millisecond)
		xReceiptMs = int64(xReceipt / time.Millisecond)
		anchorOKInt = 1
	}

	inserted := false
	wallAtReceipt := d.WallNow()
	witness, witnessOK := d.readWitness(wallAtReceipt)
	err := d.st.LocalTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO deadline_capture
			   (origin_site, node_epoch, seq, trip_id, receipt_wall_ms, remaining_ms, g_ms, x_receipt_ms, anchor_ok)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			k.origin, k.epoch, k.seq, p.TripID, wallAtReceipt.UnixMilli(),
			remainingMs, gMs, xReceiptMs, anchorOKInt,
		)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		inserted = n > 0
		if err != nil || !inserted || !witnessOK {
			return err
		}
		// R9: the witness rides the capture's own transaction — a capture
		// row never exists without the witness taken at the same instant.
		return writeWitness(tx, k, witness)
	})
	if err != nil {
		// Loud, never fatal: the pull loop must not die, and the scan's
		// no-capture rule already fails SAFE (overdue-unresolved, §9.3's
		// early direction) for exactly this trip.
		log.Printf("deadline: CAPTURE FAILED for trip %s %v: %v — trip will surface as overdue-unresolved", p.TripID, env.Key(), err)
		return
	}
	if inserted && anchorOK {
		d.mu.Lock()
		d.receipts[k] = receipt
		d.mu.Unlock()
	}
}

// captureOwnAuthored is the capture for a run THIS post authored and owns —
// a road-work run (domain/road_work.go; James, 2026-10-03). OnApplied never
// fires for it: the engine's hook runs only for envelopes pulled from a
// peer (repl/engine.go), and an own-authored event is applied by
// store.Author with no anchor and no link in between. Its time basis is
// simpler than an inbound trip's and is read off the log itself:
//
//	receipt == authoring   this post's clock stamped authored_at_wall_ms
//	                       on the envelope row (SchemaVersion 10: the time
//	                       basis rides the envelope) — the same box, the
//	                       same clock the scan's wall bridge reads now;
//	G == 0                 nothing was in flight: there is no link-down
//	                       interval between author and receipt;
//	X == the limit         the configured working limit, authored into
//	                       deadline_minutes at dispatch.
//
// So remaining_ms = X, anchor_ok = 1, and classify's branch 5 runs unchanged
// through the WALL BRIDGE (receipt_wall_ms = authored_at), which already
// refuses a backwards or implausible clock into the review lane. Nothing is
// invented: ruling 3's "the scan never invents receipt times" is about a
// peer's event whose receipt instant was never recorded; here the receipt
// instant IS recorded, by the only clock that could have, in the log row
// this scan is evaluating. INSERT OR IGNORE on the source key, like every
// capture: written once, never re-anchored. found=false (the trip row is
// known but its envelope is not in this store's log — a from-zero rebuild
// mid-replay) writes nothing, so classify lands it overdue-unresolved,
// the early-erring direction.
func (d *Deadlines) captureOwnAuthored(k key, tripID string, x time.Duration) (capture, bool, error) {
	et, found, err := d.st.EventTimeAt(store.Key{SiteID: k.origin, NodeEpoch: k.epoch, Seq: k.seq})
	if err != nil {
		return capture{}, false, fmt.Errorf("deadline: own-authored capture %s: %w", tripID, err)
	}
	if !found {
		return capture{}, false, nil
	}
	xMs := int64(x / time.Millisecond)
	// R9: the witness is taken at THIS scan, not at authoring — the boot
	// clock was not read then. The bridge therefore spans authoring → this
	// scan on the wall (one scan interval in production, the same span the
	// pre-R9 bridge trusted) and is checked from here on.
	witness, witnessOK := d.readWitness(d.WallNow())
	err = d.st.LocalTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO deadline_capture
			   (origin_site, node_epoch, seq, trip_id, receipt_wall_ms, remaining_ms, g_ms, x_receipt_ms, anchor_ok)
			 VALUES (?, ?, ?, ?, ?, ?, 0, ?, 1)`,
			k.origin, k.epoch, k.seq, tripID, et.AuthoredAtMS, xMs, xMs,
		)
		if err != nil || !witnessOK {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return writeWitness(tx, k, witness)
	})
	if err != nil {
		// Loud, never fatal to the scan: the next tick retries, and until a
		// row exists classify's no-capture branch reads the run as
		// overdue-unresolved — the safe direction.
		log.Printf("deadline: OWN-AUTHORED CAPTURE FAILED for trip %s %v: %v — run will surface as overdue-unresolved until it succeeds", tripID, k, err)
		return capture{}, false, nil
	}
	// Re-read rather than construct: a row written by an earlier scan (or an
	// earlier incarnation of the process) wins under OR IGNORE, and the
	// caller must see THAT row, not the one this call offered.
	caps, err := d.loadCaptures()
	if err != nil {
		return capture{}, false, err
	}
	c, ok := caps[k]
	return c, ok, nil
}

// capture is one persisted deadline_capture row.
type capture struct {
	tripID        string
	receiptWallMs int64
	remainingMs   sql.NullInt64
	gMs           sql.NullInt64
	xReceiptMs    sql.NullInt64
	anchorOK      bool
	// witness is the R9 clock witness taken with this capture; nil for a
	// capture written before R9 or on a host with no boot clock.
	witness *clockWitness
}

func (d *Deadlines) loadCaptures() (map[key]capture, error) {
	out := map[key]capture{}
	err := d.st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT c.origin_site, c.node_epoch, c.seq, c.trip_id,
		                              c.receipt_wall_ms, c.remaining_ms, c.g_ms, c.x_receipt_ms, c.anchor_ok,
		                              w.wall_ms, w.up_ms, w.boot_id
		                         FROM deadline_capture c
		                         LEFT JOIN deadline_clock_witness w
		                           ON w.origin_site = c.origin_site AND w.node_epoch = c.node_epoch AND w.seq = c.seq`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k key
			var c capture
			var ok int
			var wWall, wUp sql.NullInt64
			var wBoot sql.NullString
			if err := rows.Scan(&k.origin, &k.epoch, &k.seq, &c.tripID,
				&c.receiptWallMs, &c.remainingMs, &c.gMs, &c.xReceiptMs, &ok,
				&wWall, &wUp, &wBoot); err != nil {
				return err
			}
			c.anchorOK = ok == 1
			if wWall.Valid && wUp.Valid && wBoot.Valid {
				c.witness = &clockWitness{wallMs: wWall.Int64, upMs: wUp.Int64, boot: wBoot.String}
			}
			out[k] = c
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("deadline: load captures: %w", err)
	}
	return out, nil
}
