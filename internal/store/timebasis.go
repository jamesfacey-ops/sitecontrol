// timebasis.go — the RE-POINTED time-index read API (SchemaVersion 10).
//
// Before v10 these reads answered from the site-local event_time table,
// which held a clock reading only for events THIS post authored itself —
// site B held A's events but not A's authoring times, and a dead box took
// its history's time basis with it. That table, its append-only triggers,
// its writer and its version-gate exemption are DELETED (item 6): the time
// basis now rides the Envelope itself, inside Hash(), as five event_log
// columns, so any post holding an event holds its time and a wipe-and-replay
// recovers both together (schema-10 §5B).
//
// The public read API survives, re-pointed at event_log:
//
//   - EventTimeAt: the point read for one event's authoring basis.
//   - TimeIndexCoverage: the per-stream honesty instrument. Under v10 every
//     logged event carries a time BY CONSTRUCTION (the columns are NOT NULL
//     and the log started empty at the bump, D-29), so Stamped == Logged
//     always; the Logged/Stamped split is kept so callers render coverage
//     from measurement rather than assumption.
//   - SeqAsOf: resolve an instant to a sequence point for one stream.
//
// These are audit/display reads over recorded data (TP §4.4's PERMITTED
// side): they report about events already placed. Ordering remains
// (site_id, node_epoch, seq) everywhere; nothing here feeds a fold, a
// watermark, or replay selection.
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// EventTime is one event's authoring time basis: when its authoring post's
// own clock said it was authored, and how far that clock could be trusted at
// that instant. Since v10 this exists for EVERY event in the log — own and
// peer-authored alike — because it replicates inside the envelope.
type EventTime struct {
	Key          Key
	AuthoredAtMS int64
	MaxErrorUS   int64
	State        ClockState
}

// Trusted reports whether the kernel vouched for the clock at this instant.
// It is false for both 'unsynced' and 'unverified' — and for any
// unrecognised state, which fails closed the same way (§5B.4): a caller
// that renders a time must treat "the kernel says the clock is wrong",
// "we never got to ask", and "we do not recognise this claim" alike.
func (e EventTime) Trusted() bool { return e.State == ClockSynced }

// EventTimeAt returns the recorded authoring basis for one event.
// found=false means the event is not in this store's log (not yet
// replicated, or never existed) — an ordinary answer, not an error.
func (s *Store) EventTimeAt(k Key) (EventTime, bool, error) {
	var (
		et    = EventTime{Key: k}
		state string
	)
	err := s.conn().QueryRow(
		`SELECT authored_at_wall_ms, authored_at_max_error_us, authored_at_state FROM event_log
		  WHERE site_id = ? AND node_epoch = ? AND seq = ?`,
		k.SiteID, k.NodeEpoch, k.Seq).Scan(&et.AuthoredAtMS, &et.MaxErrorUS, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return EventTime{Key: k}, false, nil
	}
	if err != nil {
		return EventTime{}, false, fmt.Errorf("store: read event time %v: %w", k, err)
	}
	et.State = ClockState(state)
	return et, true, nil
}

// TimeIndexCoverage answers "what can the time basis actually speak to for
// this stream", so a view can say so instead of implying coverage it does
// not have. Under v10 every row carries a basis, so Stamped == Logged; the
// shape is kept so pages keep rendering coverage from a measurement.
type TimeIndexCoverage struct {
	// Logged is how many events this stream holds in the event log.
	Logged int64
	// Stamped is how many of those carry an authoring time. Equal to Logged
	// under v10 by construction; measured, not assumed.
	Stamped int64

	// FirstSeq/FirstAtMS is the oldest stamped event: the floor of what any
	// time-based question can reach. Zero when Stamped == 0.
	FirstSeq   int64
	FirstAtMS  int64
	LastSeq    int64
	LastAtMS   int64
	WorstState ClockState
}

// Complete reports whether every event in this stream carries a time.
func (c TimeIndexCoverage) Complete() bool { return c.Logged == c.Stamped }

// Unstamped is how many events in this stream can be addressed only by
// sequence. Render this, do not swallow it.
func (c TimeIndexCoverage) Unstamped() int64 { return c.Logged - c.Stamped }

// TimeIndexCoverage measures one stream. siteID/nodeEpoch identify a stream
// because seq orders only within an epoch (§8.3, epoch-major); there is no
// total order across epochs by seq and this read does not invent one.
func (s *Store) TimeIndexCoverage(siteID string, nodeEpoch int64) (TimeIndexCoverage, error) {
	var c TimeIndexCoverage
	var (
		first, last     sql.NullInt64
		firstAt, lastAt sql.NullInt64
		worst           sql.NullInt64
	)
	if err := s.conn().QueryRow(
		`SELECT COUNT(*),
		        MIN(seq), MAX(seq),
		        (SELECT authored_at_wall_ms FROM event_log
		          WHERE site_id = ? AND node_epoch = ? ORDER BY seq ASC  LIMIT 1),
		        (SELECT authored_at_wall_ms FROM event_log
		          WHERE site_id = ? AND node_epoch = ? ORDER BY seq DESC LIMIT 1),
		        MAX(`+clockStateRank+`)
		   FROM event_log WHERE site_id = ? AND node_epoch = ?`,
		siteID, nodeEpoch, siteID, nodeEpoch, siteID, nodeEpoch,
	).Scan(&c.Logged, &first, &last, &firstAt, &lastAt, &worst); err != nil {
		return TimeIndexCoverage{}, fmt.Errorf("store: time index coverage (%s, %d): %w", siteID, nodeEpoch, err)
	}
	// v10: every logged row carries a basis (NOT NULL columns; empty log at
	// the bump), so the stamped count IS the logged count.
	c.Stamped = c.Logged
	c.FirstSeq, c.FirstAtMS = first.Int64, firstAt.Int64
	c.LastSeq, c.LastAtMS = last.Int64, lastAt.Int64
	c.WorstState = rankToState(worst)
	return c, nil
}

// clockStateRank orders the states worst-last so SQL MAX picks the worst one
// in a range. 'unsynced' ranks above 'unverified' because it is a POSITIVE
// statement by the kernel that the clock is not disciplined, whereas
// 'unverified' is an absence of information; any unrecognised state ranks
// with 'unsynced' (fail closed — the ELSE arm). All non-synced states must
// render as "do not trust this time"; the ranking only decides which one a
// summary names when a range contains several.
const clockStateRank = `CASE authored_at_state WHEN 'synced' THEN 0 WHEN 'unverified' THEN 1 ELSE 2 END`

func rankToState(r sql.NullInt64) ClockState {
	if !r.Valid {
		return ClockUnverified
	}
	switch r.Int64 {
	case 0:
		return ClockSynced
	case 1:
		return ClockUnverified
	default:
		return ClockUnsynced
	}
}

// AsOf is the answer to a time-based question about one stream: the sequence
// point to fold up to, and what the clock was worth across everything folded.
type AsOf struct {
	// Seq is the greatest stamped seq authored at or before the asked-for
	// instant. Fold the stream to this seq (bounded replay through
	// Events + ApplyBatch into a scratch store already reconstructs state at
	// a sequence point — that half needs nothing new).
	Seq int64

	// Found is false when nothing in this stream was authored at or before
	// the instant asked about.
	Found bool

	// BeforeCoverage is true when the instant asked about is EARLIER than
	// the first stamped event in this stream — e.g. the question predates
	// this epoch. A caller that treats BeforeCoverage as "nobody was here"
	// is wrong: say the coverage does not reach, do not draw a blank sheet.
	BeforeCoverage bool

	// WorstState / MaxErrorUS describe the clock across the WHOLE stamped
	// prefix being folded, not just the boundary event, because the answer
	// is built from all of it. WorstState != ClockSynced means the sheet
	// must carry an explicit "this time may be wrong" marking.
	WorstState ClockState
	MaxErrorUS int64
}

// SeqAsOf resolves an instant to a sequence point for one stream: the
// greatest seq whose authoring time is at or before atMS.
//
// Deliberately MAX(seq) over the time filter rather than "the last seq before
// the first event later than atMS". The two differ only when the clock stepped
// backwards mid-stream, and MAX(seq) is the safer of the two there: it yields
// a sequence PREFIX (which is the only thing a fold can replay), and it
// errs toward INCLUDING an event whose recorded time looks late rather than
// dropping a person off a historical muster. Under a 100%-accountability
// obligation, an extra name that can be explained beats a missing one that
// cannot. The clock excursion that caused it is not hidden: it shows up as a
// degraded WorstState/MaxErrorUS on the same answer.
//
// This does NOT search across epochs. A re-image starts a new stream; asking
// an instant of the wrong epoch's stream correctly reports BeforeCoverage or
// Found=false rather than silently crossing a boundary that has no ordering.
//
// This is an audit-side read over recorded data (TP §4.4 PERMITTED): its
// answer changes what a page reports about events already placed, never
// which events fold or what a watermark says.
func (s *Store) SeqAsOf(siteID string, nodeEpoch, atMS int64) (AsOf, error) {
	fail := func(err error) (AsOf, error) {
		return AsOf{}, fmt.Errorf("store: seq as of %d (%s, epoch %d): %w", atMS, siteID, nodeEpoch, err)
	}
	var found sql.NullInt64
	if err := s.conn().QueryRow(
		`SELECT MAX(seq) FROM event_log
		  WHERE site_id = ? AND node_epoch = ? AND authored_at_wall_ms <= ?`,
		siteID, nodeEpoch, atMS).Scan(&found); err != nil {
		return fail(err)
	}
	if !found.Valid {
		// Nothing at or before this instant. Distinguish "this stream holds
		// nothing at all" from "the question predates the stream's coverage"
		// — they read very differently on a page.
		var any sql.NullInt64
		if err := s.conn().QueryRow(
			`SELECT MIN(authored_at_wall_ms) FROM event_log WHERE site_id = ? AND node_epoch = ?`,
			siteID, nodeEpoch).Scan(&any); err != nil {
			return fail(err)
		}
		return AsOf{Found: false, BeforeCoverage: any.Valid && atMS < any.Int64}, nil
	}
	out := AsOf{Seq: found.Int64, Found: true}
	var (
		worst  sql.NullInt64
		maxErr sql.NullInt64
	)
	if err := s.conn().QueryRow(
		`SELECT MAX(`+clockStateRank+`), MAX(authored_at_max_error_us) FROM event_log
		  WHERE site_id = ? AND node_epoch = ? AND seq <= ?`,
		siteID, nodeEpoch, out.Seq).Scan(&worst, &maxErr); err != nil {
		return fail(err)
	}
	out.WorstState = rankToState(worst)
	out.MaxErrorUS = MaxErrorUnknown
	if maxErr.Valid {
		out.MaxErrorUS = maxErr.Int64
	}
	return out, nil
}

// MinAuthoredAtWallMS returns the earliest authoring instant across the
// WHOLE local event_log — every stream, own-authored and applied peers'
// rows alike: "this box's records" (TP §4.6 rule 2, the records floor's
// DEFINED comparator: the floor warning fires iff
// entered < min(authored_at) over the local event_log). ok is false when
// the log is empty — no records, no floor: the bound is suppressed rather
// than computed against an undefined aggregate.
//
// This is an audit-side read over recorded data (TP §4.4 PERMITTED): its
// answer decides whether a WARNING renders beside an accepted value; it
// never orders events, never gates authoring (the floor never refuses),
// and never touches OccurredAt/TimeSource.
func (s *Store) MinAuthoredAtWallMS() (int64, bool, error) {
	var min sql.NullInt64
	if err := s.conn().QueryRow(
		`SELECT MIN(authored_at_wall_ms) FROM event_log`).Scan(&min); err != nil {
		return 0, false, fmt.Errorf("store: min authored_at: %w", err)
	}
	if !min.Valid {
		return 0, false, nil
	}
	return min.Int64, true, nil
}
