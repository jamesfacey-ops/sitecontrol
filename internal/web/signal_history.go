// signal_history.go — the lost-signal history (FIX16f, James 2026-10-06:
// "which sites lost signal, when, and for how long"), recorded GOING FORWARD
// in a site-local table.
//
// WHY IT CANNOT BE RECONSTRUCTED. Checked before building this, against
// every record that exists:
//   - Sync status is in memory only (repl.Snapshot: per-peer contact ages
//     since this process started). Nothing about past pulls, pokes or
//     their failures is persisted.
//   - The event log holds each event's AUTHORING time by its origin's clock
//     and no receipt time at all (schema-10 §5B.7 deleted the site-local
//     event_time index). A gap between a peer's events is a quiet post as
//     often as a cut link, so it cannot tell an outage from a night shift.
//   - deadline_capture holds a receipt instant, but only for runs bound for
//     this post: a handful of points a day, which bound nothing.
//
// So past outages are not in this store, and none are invented.
//
// THE MECHANISM: store.RegisterLocalSchema / LocalTx, the C7 site-local
// seam deadline_capture and R9's deadline_clock_witness use. The table lives
// in the post's own SQLite file, is never folded, never replicated and never
// dumped (store/local.go), so it needs no SchemaVersion bump — store.go's
// bump log records exactly that for x_receipt_ms and deadline_red_fired.
// Each post records its OWN view: which peers IT lost contact with. The
// dispatch page shows the serving post's record (NDB's, in the demo).
//
// THE RULE is the top bar's, unchanged (lostsignal.go peerView.Lost): no
// completed sync for more than the STALE ceiling. A recorder samples the
// sync snapshot every sync interval. An outage starts at the peer's LAST
// CONTACT (or at this post's start, when it has not heard the peer since —
// marked as such), is noted when the ceiling is crossed, and ends at the
// first completed sync after it. Wall times are this post's clock, held
// only for display (local.go: never an ordering or comparison input).
//
// A RESTART. An outage open when this post stopped stays open; the next
// process closes it at the first contact it makes, and the row says this
// post restarted during it (the gap includes this post's own downtime).
package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

const signalOutageSchema = `
CREATE TABLE IF NOT EXISTS signal_outage (
    peer_site     TEXT    NOT NULL,
    lost_from_ms  INTEGER NOT NULL,
    never_heard   INTEGER NOT NULL CHECK (never_heard IN (0, 1)),
    detected_ms   INTEGER NOT NULL,
    ended_ms      INTEGER,
    opened_run_ms INTEGER NOT NULL,
    closed_run_ms INTEGER,
    PRIMARY KEY (peer_site, detected_ms)
) STRICT;`

// RegisterSignalHistory creates the site-local table. Startup only, like
// deadline.New's local schemas; main calls it before RunSignalRecorder.
func RegisterSignalHistory(st *store.Store) error {
	return st.RegisterLocalSchema("lost-signal history (FIX16f)", signalOutageSchema)
}

// errSignalNotRecorded: the table does not exist in this store — no
// recorder has ever run here.
var errSignalNotRecorded = errors.New("this post has no lost-signal record")

// statusSignalRows is how many outages the status page lists.
const statusSignalRows = 10

// signalOutage is one row.
type signalOutage struct {
	Peer        string
	LostFromMS  int64
	NeverHeard  bool
	DetectedMS  int64
	EndedMS     int64 // 0 = still lost
	OpenedRunMS int64
	ClosedRunMS int64
}

// Open reports an outage still in progress.
func (o signalOutage) Open() bool { return o.EndedMS == 0 }

// SpansRestart reports an outage closed by a later process than the one
// that noted it: this post was itself down for part of it.
func (o signalOutage) SpansRestart() bool { return !o.Open() && o.ClosedRunMS != o.OpenedRunMS }

// RunSignalRecorder samples the sync snapshot every sync interval until ctx
// ends. A failed write is logged and retried on the next tick.
func (s *Server) RunSignalRecorder(ctx context.Context) {
	every := s.Cfg.SyncInterval()
	if every <= 0 {
		every = 10 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.recordSignal(time.UnixMilli(s.clockReading().WallMS)); err != nil {
			log.Printf("lost-signal history: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// recordSignal is one sample at now (this post's clock).
func (s *Server) recordSignal(now time.Time) error {
	v := s.buildSyncSection()
	if !v.Available {
		return nil
	}
	nowMS := now.UnixMilli()
	runMS := now.Add(-v.Uptime).UnixMilli()
	return s.Store.LocalTx(func(tx *sql.Tx) error {
		open := map[string]signalOutage{}
		rows, err := tx.Query(`SELECT peer_site, lost_from_ms, detected_ms FROM signal_outage WHERE ended_ms IS NULL`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var o signalOutage
			if err := rows.Scan(&o.Peer, &o.LostFromMS, &o.DetectedMS); err != nil {
				rows.Close()
				return err
			}
			open[o.Peer] = o
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, p := range v.Peers {
			o, isOpen := open[p.SiteID]
			switch {
			case p.Lost && !isOpen:
				from, never := runMS, 1
				if p.ContactAgo != nil {
					from, never = nowMS-int64(*p.ContactAgo*1000), 0
				}
				if _, err := tx.Exec(`INSERT OR IGNORE INTO signal_outage (peer_site, lost_from_ms, never_heard, detected_ms, opened_run_ms) VALUES (?, ?, ?, ?, ?)`,
					p.SiteID, from, never, nowMS, runMS); err != nil {
					return err
				}
			case !p.Lost && isOpen && p.ContactAgo != nil:
				// Back: the first completed sync after the outage. A contact
				// no newer than the outage's start (only a key fault's
				// "not lost") ends it now instead.
				ended := nowMS - int64(*p.ContactAgo*1000)
				if ended <= o.LostFromMS {
					ended = nowMS
				}
				if _, err := tx.Exec(`UPDATE signal_outage SET ended_ms = ?, closed_run_ms = ? WHERE peer_site = ? AND detected_ms = ?`,
					ended, runMS, p.SiteID, o.DetectedMS); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// signalOutages reads the record, newest first. errSignalNotRecorded when
// the table was never created here.
func (s *Server) signalOutages() ([]signalOutage, error) {
	var out []signalOutage
	err := s.Store.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT peer_site, lost_from_ms, never_heard, detected_ms, COALESCE(ended_ms, 0), opened_run_ms, COALESCE(closed_run_ms, 0)
		                         FROM signal_outage ORDER BY lost_from_ms DESC, peer_site`)
		if err != nil {
			if strings.Contains(err.Error(), "no such table") {
				return errSignalNotRecorded
			}
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o signalOutage
			var never int
			if err := rows.Scan(&o.Peer, &o.LostFromMS, &never, &o.DetectedMS, &o.EndedMS, &o.OpenedRunMS, &o.ClosedRunMS); err != nil {
				return err
			}
			o.NeverHeard = never == 1
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil && !errors.Is(err, errSignalNotRecorded) {
		err = fmt.Errorf("lost-signal history: %w", err)
	}
	return out, err
}
