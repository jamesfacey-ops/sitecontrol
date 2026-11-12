// stream.go — Amendment 2's stream: EachEvent, THE one audit read over the
// log, plus the stream census (Watermarks) a whole-log consumer needs to
// know which streams to walk.
//
// EachEvent's signature is the one settled jointly by historical-muster-v1
// §11.2 and analytics-v1 §4.2 (PLAN-SEC-A-schema.md Amendment 2), verbatim:
// one stream, key order, materialises nothing, toSeq <= 0 = to the end. It
// was pre-named on W1's guard-3b reader allowlist
// (w1_eventtime_gone_test.go) precisely so that landing it extends nothing.
// Its first consumer is domain/visit_report.go's replay; the rule the plan
// states — no reader outside that allowlist — is what moved that report's
// log read from a raw query in package domain to this method.
//
// These are audit-side reads over recorded data (TP §4.4's PERMITTED side):
// they report about events already placed. Ordering is (site_id,
// node_epoch, seq); nothing here feeds a fold, a watermark, or replay
// selection, and nothing here reads OccurredAt/TimeSource — the envelope is
// handed to fn intact and the consumer classifies it through the domain's
// one-place helpers.
package store

import "fmt"

// EachEvent streams one stream's events in (site_id, node_epoch, seq) order
// to fn, materialising nothing. toSeq <= 0 means "to the end". Returning a
// non-nil error from fn stops the walk and is returned. This is the ONE
// audit read over the log: analytics accumulates statistics over it, and
// the historical muster derives visit spans over it. There must not be a
// second.
//
// fn runs while the read cursor is open on the store's single connection,
// so it must not touch the store (no Author, no LocalTx, no other read):
// accumulate in memory and query afterwards. An unknown stream yields no
// calls and no error — an ordinary answer, like Events.
func (s *Store) EachEvent(siteID string, nodeEpoch, fromSeq, toSeq int64, fn func(Envelope) error) error {
	s.countRead()
	q := `SELECT site_id, node_epoch, seq, schema_version, origin_auth,
	             authored_at_wall_ms, authored_at_max_error_us, authored_at_state,
	             occurred_at, time_source, payload
	        FROM event_log
	       WHERE site_id = ? AND node_epoch = ? AND seq >= ?`
	args := []any{siteID, nodeEpoch, fromSeq}
	if toSeq > 0 {
		q += ` AND seq <= ?`
		args = append(args, toSeq)
	}
	q += ` ORDER BY seq`
	rows, err := s.conn().Query(q, args...)
	if err != nil {
		return fmt.Errorf("store: each event (%s, %d, %d..%d): %w", siteID, nodeEpoch, fromSeq, toSeq, err)
	}
	// ⟦PANIC-1⟧ the one read that keeps the store's single connection across
	// a caller's callback: recorded as its holder for the reentrancy guard's
	// verdict (reentrancy_guard.go), released after rows.Close (defers run
	// last-in first-out). A Transact view already holds its transaction.
	if s.tx == nil && !s.pool.usable() {
		defer s.holdConn("EachEvent")() // ⟦FIX17c⟧ only when it is the writer's connection
	}
	defer rows.Close()
	for rows.Next() {
		var (
			env                Envelope
			payload, authState string
		)
		if err := rows.Scan(&env.SiteID, &env.NodeEpoch, &env.Seq, &env.SchemaVersion, &env.OriginAuth,
			&env.AuthoredAt.WallMS, &env.AuthoredAt.MaxErrorUS, &authState,
			&env.OccurredAt, &env.TimeSource, &payload); err != nil {
			return fmt.Errorf("store: each event: scan: %w", err)
		}
		env.AuthoredAt.State = ClockState(authState)
		env.Payload = []byte(payload)
		if err := fn(env); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Stream is one replicated stream this store holds and how far it has been
// contiguously applied — the census a whole-log walk iterates, and the
// coverage figure a report prints beside a peer site's numbers.
type Stream struct {
	SiteID     string
	NodeEpoch  int64
	AppliedSeq int64
}

// Watermarks lists every stream in (site_id, node_epoch) order. Because a
// row lands in event_log only inside the transaction that advances the
// watermark (insertAndFold), a stream's log rows are exactly 1..AppliedSeq,
// so walking each listed stream with EachEvent(…, 1, 0, fn) visits the
// whole log with no stream missed. Empty store, empty list, nil error.
func (s *Store) Watermarks() ([]Stream, error) {
	s.countRead()
	rows, err := s.conn().Query(`SELECT site_id, node_epoch, applied_seq FROM watermark ORDER BY site_id, node_epoch`)
	if err != nil {
		return nil, fmt.Errorf("store: watermarks: %w", err)
	}
	defer rows.Close()
	var out []Stream
	for rows.Next() {
		var m Stream
		if err := rows.Scan(&m.SiteID, &m.NodeEpoch, &m.AppliedSeq); err != nil {
			return nil, fmt.Errorf("store: watermarks: scan: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
