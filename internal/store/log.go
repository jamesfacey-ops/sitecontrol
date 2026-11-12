// log.go — own-stream authoring, the idempotent apply loop, and the watermark
// store with its contiguity rule (chunk-plan AR-5: C2 owns these; C3a's pull
// path consumes them and never redefines them).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Author appends the next event on this site's OWN stream. Seq assignment is
// contiguous per (siteID, nodeEpoch): the next seq is always that stream's
// watermark + 1, computed and committed in the same transaction as the log
// row and the folds, so a crash can never burn or skip a number and the
// incrementally-built state is BY CONSTRUCTION a fold over the log.
//
// SchemaVersion is stamped from store.SchemaVersion (the single source, C1
// review M1). OriginAuth carries the authoring site's own identity — the
// claim C3a's ingest rule (§8.4) checks against the delivering peer.
//
// The time basis (SchemaVersion 10, items 6 + 13): Author reads s.Clock
// EXACTLY ONCE and stamps AuthoredAt from that single reading, normalising
// an unrecognised clock state to unverified BEFORE hashing (schema-10 §5B.4
// rule 2 — own-authored events always carry one of the three frozen states;
// an event is never refused for a clock reason). The OccurredAt/TimeSource
// pair is the caller's provenance claim (TP §4.1):
//
//   - timeSource "default": the officer left the time untouched. RULE W —
//     OccurredAt := AuthoredAt.WallMS, a COPY from that same single reading,
//     never a second time.Now(); occurredAtMS (the form's echoed default) is
//     IGNORED and never stored (TP §4.5). The default invariant
//     OccurredAt == AuthoredAt.WallMS is an exact equality, never a
//     tolerance.
//   - any other timeSource (notably "entered"): occurredAtMS is stored
//     byte-exactly, as is the timeSource string itself — no write-side
//     normalisation, ever: the values sit inside Hash().
func (s *Store) Author(siteID string, nodeEpoch int64, payload json.RawMessage, occurredAtMS int64, timeSource string) (Envelope, error) {
	// Canonicalise the payload to the WIRE encoding before anything is
	// hashed or stored (campaign finding F9). Go's JSON encoder rewrites
	// json.RawMessage in flight — insignificant whitespace stripped, <, >, &
	// and U+2028/U+2029 escaped — so a valid-but-non-canonical payload would
	// store differently at a replica (which stores the re-encoded wire
	// bytes) than at its author, and the seam-echo hash would then report
	// prefix_diverged on fully honest traffic. json.Marshal on a RawMessage
	// applies exactly the compact-and-escape pass the wire's
	// json.NewEncoder(...).Encode performs, and is idempotent, so stored
	// bytes ≡ wire bytes by construction for every caller, however the
	// payload was built. Empty or invalid JSON is left untouched for
	// Validate below to refuse with the standard error (json.Marshal would
	// silently turn a nil RawMessage into "null" — that must stay a refusal).
	if len(payload) > 0 && json.Valid(payload) {
		if canon, cerr := json.Marshal(payload); cerr == nil {
			payload = canon
		}
	}
	// THE one clock reading (rule W: copy, never re-read). Taken before the
	// transaction opens; the trust bound moves on a scale of minutes, and a
	// single reading is what makes the default invariant an equality.
	clock := s.Clock
	if clock == nil {
		clock = SystemClock
	}
	reading := clock()
	// Authoring-path normalisation, BEFORE hashing (schema-10 §5B.4 rule 2):
	// own-authored events always carry one of the three frozen states. This
	// is the ONLY normalisation anywhere on any write path — an inbound
	// envelope (ApplyBatch) is stored exactly as received.
	switch reading.State {
	case ClockSynced, ClockUnsynced, ClockUnverified:
	default:
		reading.State = ClockUnverified
	}
	env := Envelope{
		SiteID:        siteID,
		NodeEpoch:     nodeEpoch,
		SchemaVersion: SchemaVersion,
		OriginAuth:    siteID,
		Payload:       payload,
	}
	env.AuthoredAt = AuthoredAt{WallMS: reading.WallMS, MaxErrorUS: reading.MaxErrorUS, State: reading.State}
	if timeSource == "default" {
		// Rule W: copy from the same single reading; the caller's echoed
		// value is never stored.
		env.OccurredAt = reading.WallMS
	} else {
		env.OccurredAt = occurredAtMS
	}
	env.TimeSource = timeSource
	// ⟦A3, 2026-09-29⟧ INSIDE A Transact VIEW the event joins the caller's
	// transaction instead of opening its own: same seq rule (watermark + 1
	// as the transaction now sees it, so the second event of a commit is
	// wm+2), same Validate, same D-87 choke point, same fold and Finalize —
	// under a SAVEPOINT so this one event still lands whole or not at all,
	// and committed by whoever opened the transaction. See transact.go.
	if s.tx != nil {
		if err := savepoint(s.tx, "sc_author", func() error { return s.authorOn(s.tx, &env) }); err != nil {
			return Envelope{}, err
		}
		s.authored = true // the view's commit (Transact) fires OnAuthored once for the whole commit
		return env, nil
	}
	// OnAuthored (W7 / Y-17): registered BEFORE begin so it runs LAST — after
	// tx.Rollback (a no-op once committed) and after release() has freed the
	// single connection — and only if the commit actually happened.
	committed := false
	defer func() {
		if committed {
			s.fireOnAuthored()
		}
	}()
	tx, release, err := s.begin("Author")
	if err != nil {
		return Envelope{}, fmt.Errorf("store: author: %w", err)
	}
	defer release()
	defer tx.Rollback()
	if err := s.authorOn(tx, &env); err != nil {
		return Envelope{}, err
	}
	if err := s.commit(tx, true); err != nil {
		return Envelope{}, fmt.Errorf("store: author commit: %w", err)
	}
	committed = true
	return env, nil
}

// authorOn is Author's transactional middle — seq assignment, Validate, the
// D-87 choke point, the append + folds, Finalize — on the caller's
// transaction, which the caller commits or discards. Shared byte-for-byte
// by the standalone path (one transaction per event) and the Transact view
// (one transaction per commit), so the two cannot drift.
func (s *Store) authorOn(tx *sql.Tx, env *Envelope) error {
	wm, err := watermarkTx(tx, env.SiteID, env.NodeEpoch)
	if err != nil {
		return err
	}
	env.Seq = wm + 1
	if err := env.Validate(); err != nil {
		return err
	}
	// D-87 choke point (ownership.go): a site modifies only rows carrying
	// its own tag. HERE, and only here — after Validate, before the append,
	// inside this transaction, on the path that carries own-authored events
	// alone. Never in insertAndFold, applyBatch or a fold: those also carry
	// peer events and this post's replayed history, and a refusal there
	// forks the logs (W3-2). The deferred Rollback discards the transaction
	// on refusal, so nothing is written — not the row, not the watermark.
	if err := s.refuseForeignRow(tx, env.SiteID, env.Payload); err != nil {
		return err
	}
	if err := s.insertAndFold(tx, *env); err != nil {
		return err
	}
	// SchemaVersion 10: the time basis rides the envelope row itself, so the
	// event and its time are atomic BY CONSTRUCTION — the separate
	// event_time stamp (and its crash-window reasoning) is gone with its
	// table. Apply still stamps nothing: a peer's envelope arrives with its
	// own authoring post's basis and is stored exactly as received, never
	// re-stamped from this box's clock (§9.3 rule 4; TP §4.1 rule 2).
	return s.finalize(tx, []Envelope{*env})
}

// Apply is the idempotent apply loop for delivered events, keyed on the FULL
// (site_id, node_epoch, seq) triple (§8.4). Semantics, per stream:
//
//   - seq <= watermark:  duplicate — no projection mutation, no log write,
//     (false, nil). Redelivering an entire stream is a no-op.
//   - seq == watermark+1: applied — log insert, every registered fold, and
//     the watermark advance commit in ONE transaction; returns (true, nil).
//   - seq >  watermark+1: gap — *GapError, nothing applied, watermark
//     stalled loudly (§8.4: never a silent skip).
//
// An envelope stamped with a foreign schema_version is refused with
// *SchemaVersionError before anything is written — the log never holds events
// of another schema version (C3a layers the §8.5 queue + visible refusal on
// top of this typed error).
//
// Apply does not care whose site_id the envelope bears: injecting foreign-site
// events in tests is exactly what makes C5/C6 buildable before replication
// exists. Origin-identity enforcement is C3a's, at the transport.
func (s *Store) Apply(env Envelope) (applied bool, err error) {
	results, err := s.ApplyBatch([]Envelope{env})
	return len(results) == 1 && results[0], err
}

// ApplyBatch applies a slice of delivered envelopes in ONE transaction, in
// order, each under exactly Apply's per-envelope rules (idempotent duplicate /
// applied / typed gap or schema refusal), then runs every registered
// projection's Finalize once over the applied set before the commit — the F28
// per-drain batch seam. results[i] reports whether envs[i] was applied (as
// opposed to a duplicate); len(results) < len(envs) exactly when an envelope
// failed, in which case the APPLIED PREFIX IS COMMITTED (finalized first) and
// the failing envelope's typed error is returned — the same watermark-rests-
// at-the-last-good-seq outcome the per-envelope loop produced, so one
// poisoned envelope stalls a stream without burning the good prefix into an
// every-cycle re-pull.
//
// Batch atomicity: raw rows, Finalize output and watermark advances commit
// together, so no reader — and no crash — can ever observe raw rows whose
// derived state has not caught up, exactly the invariant the per-event
// transaction gave. A crash mid-batch re-pulls the whole page through the
// idempotent path.
func (s *Store) ApplyBatch(envs []Envelope) (results []bool, err error) {
	return s.applyBatch(envs, true)
}

// applyBatch is ApplyBatch plus the fold-error unwind (verification finding
// on the F28 landing): a fold error mid-envelope must leave NO trace of the
// failing envelope — the old per-envelope transaction rolled its partial
// writes back, and committing them with the prefix would leave state where
// derived is not f(raw) (e.g. a poisoned arrival's committed OPEN visit on
// the muster with no derived row, and an event_log row above the watermark
// that turns every retry into a PRIMARY KEY conflict masking the true
// error). Rather than pay a per-envelope SAVEPOINT round-trip on the hot
// path (measured ~35µs/event, +36% on a season replay), the whole batch
// transaction is rolled back on a fold error and the CLEAN PREFIX is
// re-applied once through this same function — folds are pure functions of
// transaction state, so the prefix re-applies identically (the project's
// core determinism property; the one-retry bound means even a
// nondeterministic fold failure aborts loudly rather than looping). The
// happy path — every batch that contains no poisoned envelope — pays
// nothing.
func (s *Store) applyBatch(envs []Envelope, retryPrefix bool) (results []bool, err error) {
	if len(envs) == 0 {
		return nil, nil
	}
	if s.tx != nil {
		return nil, errApplyInsideTransact
	}
	tx, release, err := s.begin("ApplyBatch")
	if err != nil {
		return nil, fmt.Errorf("store: apply: %w", err)
	}
	defer release()
	defer tx.Rollback()

	results = make([]bool, 0, len(envs))
	applied := make([]Envelope, 0, len(envs))
	var envErr error
	for i, env := range envs {
		if err := env.Validate(); err != nil {
			envErr = err
			break
		}
		if env.SchemaVersion != SchemaVersion {
			envErr = &SchemaVersionError{Key: env.Key(), Got: env.SchemaVersion, Want: SchemaVersion}
			break
		}
		wm, err := watermarkTx(tx, env.SiteID, env.NodeEpoch)
		if err != nil {
			envErr = err
			break
		}
		switch {
		case env.Seq <= wm:
			results = append(results, false) // already applied: idempotent duplicate
			continue
		case env.Seq > wm+1:
			envErr = &GapError{Key: env.Key(), Watermark: wm}
		}
		if envErr != nil {
			break
		}
		if err := s.insertAndFold(tx, env); err != nil {
			// The failing envelope has left partial writes in this
			// transaction. Discard EVERYTHING, then re-apply the clean
			// prefix (envs[:i]) once — see the function comment.
			tx.Rollback()
			if !retryPrefix {
				return nil, err
			}
			prefixResults, retryErr := s.applyBatch(envs[:i], false)
			if retryErr != nil {
				return nil, fmt.Errorf("store: apply %v: %v; and the clean prefix failed on re-apply: %w", env.Key(), err, retryErr)
			}
			return prefixResults, err
		}
		results = append(results, true)
		applied = append(applied, env)
	}
	if err := s.finalize(tx, applied); err != nil {
		// A Finalize failure poisons the whole batch: derived state cannot
		// be reconciled with the raw rows, so nothing may commit.
		return nil, err
	}
	if err := s.commit(tx, len(applied) > 0); err != nil {
		return nil, fmt.Errorf("store: apply commit: %w", err)
	}
	return results, envErr
}

// finalize runs each registered projection's Finalize (projection-name
// order, like the folds) over the batch's applied envelopes. Nothing to do
// when nothing was applied.
func (s *Store) finalize(tx *sql.Tx, applied []Envelope) error {
	if len(applied) == 0 {
		return nil
	}
	for _, p := range s.projections {
		if p.Finalize == nil {
			continue
		}
		if err := p.Finalize(tx, applied); err != nil {
			return fmt.Errorf("store: finalize %q: %w", p.Name, err)
		}
	}
	return nil
}

// insertAndFold writes the log row, runs every registered fold, and advances
// the watermark — all on the caller's transaction. Folds run in projection
// name order (one deterministic order everywhere).
func (s *Store) insertAndFold(tx *sql.Tx, env Envelope) error {
	if _, err := tx.Exec(
		`INSERT INTO event_log (site_id, node_epoch, seq, schema_version, origin_auth,
		                        authored_at_wall_ms, authored_at_max_error_us, authored_at_state,
		                        occurred_at, time_source, payload, row_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		// row_hash (F25 fix): Envelope.Hash() computed from the SAME env this
		// row is built from, so it is a redundant copy of a digest over this
		// row's own content — see event_log's row_hash column comment and the
		// SchemaVersion 5→6 doc comment for why this catches content-only
		// corruption that PRAGMA integrity_check does not. The five time-
		// basis values are stored EXACTLY as the envelope carries them (they
		// are inside that hash): no normalisation, no rewrite, whatever the
		// bytes say.
		env.SiteID, env.NodeEpoch, env.Seq, env.SchemaVersion, env.OriginAuth,
		env.AuthoredAt.WallMS, env.AuthoredAt.MaxErrorUS, string(env.AuthoredAt.State),
		env.OccurredAt, env.TimeSource, string(env.Payload), env.Hash(),
	); err != nil {
		return fmt.Errorf("store: append %v: %w", env.Key(), err)
	}
	for _, p := range s.projections {
		if p.Apply == nil {
			continue
		}
		if err := p.Apply(tx, env); err != nil {
			return fmt.Errorf("store: fold %q on %v: %w", p.Name, env.Key(), err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO watermark (site_id, node_epoch, applied_seq) VALUES (?, ?, ?)
		 ON CONFLICT(site_id, node_epoch) DO UPDATE SET applied_seq = excluded.applied_seq`,
		env.SiteID, env.NodeEpoch, env.Seq,
	); err != nil {
		return fmt.Errorf("store: advance watermark %v: %w", env.Key(), err)
	}
	return nil
}

func watermarkTx(tx dbtx, siteID string, nodeEpoch int64) (int64, error) {
	var wm int64
	err := tx.QueryRow(
		`SELECT applied_seq FROM watermark WHERE site_id = ? AND node_epoch = ?`,
		siteID, nodeEpoch).Scan(&wm)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // a stream never heard from opens at zero (§8.4)
	}
	if err != nil {
		return 0, fmt.Errorf("store: read watermark (%s, %d): %w", siteID, nodeEpoch, err)
	}
	return wm, nil
}

// Watermark reports the last contiguously-applied seq for a stream; 0 for a
// stream never heard from.
func (s *Store) Watermark(siteID string, nodeEpoch int64) (int64, error) {
	if s.tx != nil {
		return watermarkTx(s.tx, siteID, nodeEpoch) // a Transact view reads its own transaction
	}
	q, done, err := s.readTx("Watermark")
	if err != nil {
		return 0, fmt.Errorf("store: watermark: %w", err)
	}
	defer done()
	return watermarkTx(q, siteID, nodeEpoch)
}

// Events reads a stream from the log: "give me site S, epoch E, from seq N,
// at most L" — the read shape C3a's pull server serves (§8.4), paginated per
// the C2 review's M1 correction (the 9.3 anchor rule is defined over batches,
// so catch-up must be able to page). limit <= 0 means no limit (test/replay
// use). Ordered by seq. Because Apply inserts only at watermark+1, the log
// holds a contiguous prefix per stream, always — a page read here can never
// contain a gap.
func (s *Store) Events(siteID string, nodeEpoch, fromSeq, limit int64) ([]Envelope, error) {
	return eventsOn(s.conn(), siteID, nodeEpoch, fromSeq, limit)
}

// EventsAndHead is Events plus the stream's watermark — its head, carry-forward
// row 3 — read in ONE transaction, so the two agree: a consistent snapshot in
// which "head > from−1" implies the page carries seq from (the log is a
// contiguous prefix per stream, always).
//
// ⟦W7, 2026-10-03⟧ The pull server (repl/server.go handlePull) used to read
// the page and then the head as two separate transactions, reasoning that
// head-after-page can never produce a false "drained" — true, but the
// ordering can STRADDLE an Author commit: an empty page (seq from not yet
// committed) beside a head one past the client's watermark (committed a
// moment later). The client cannot tell that from a server that claims a
// head and serves nothing — a protocol violation it must refuse rather than
// spin on — so it stalled the stream as `state=error: author reports head N
// but the served page advanced nothing past N−1`, logged SYNC ERROR, and
// recovered on the next pull. Rare while peers pulled on a 10 s tick; once
// the §8.9 poke went live (Y-17) peers pull DURING the author's commit burst,
// and ndb logged it 4 times in a 1 260-event, 50 s burst (sc-test L12 Y-30,
// W7 run). One snapshot makes the client's premise true by construction for
// an honest author; the client-side check stays as the backstop against a
// dishonest one. Inside a Transact view both reads run on the view's own
// transaction, as every other read does.
func (s *Store) EventsAndHead(siteID string, nodeEpoch, fromSeq, limit int64) ([]Envelope, int64, error) {
	if s.tx != nil {
		evs, err := eventsOn(s.tx, siteID, nodeEpoch, fromSeq, limit)
		if err != nil {
			return nil, 0, err
		}
		head, err := watermarkTx(s.tx, siteID, nodeEpoch)
		return evs, head, err
	}
	// ⟦FIX17c⟧ one read snapshot, off the writer (readpool.go): it holds
	// only committed, fsynced events, so serving a peer from it never ships
	// an event a crash could take back.
	q, done, err := s.readTx("EventsAndHead")
	if err != nil {
		return nil, 0, fmt.Errorf("store: events+head: %w", err)
	}
	defer done()
	evs, err := eventsOn(q, siteID, nodeEpoch, fromSeq, limit)
	if err != nil {
		return nil, 0, err
	}
	head, err := watermarkTx(q, siteID, nodeEpoch)
	if err != nil {
		return nil, 0, err
	}
	return evs, head, nil
}

// eventsOn is Events' query on the caller's connection or transaction.
func eventsOn(q dbtx, siteID string, nodeEpoch, fromSeq, limit int64) ([]Envelope, error) {
	query := `SELECT site_id, node_epoch, seq, schema_version, origin_auth,
		         authored_at_wall_ms, authored_at_max_error_us, authored_at_state,
		         occurred_at, time_source, payload
		   FROM event_log
		  WHERE site_id = ? AND node_epoch = ? AND seq >= ?
		  ORDER BY seq`
	args := []any{siteID, nodeEpoch, fromSeq}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read events (%s, %d, from %d): %w", siteID, nodeEpoch, fromSeq, err)
	}
	defer rows.Close()
	var out []Envelope
	for rows.Next() {
		var env Envelope
		var payload, authState string
		if err := rows.Scan(&env.SiteID, &env.NodeEpoch, &env.Seq, &env.SchemaVersion, &env.OriginAuth,
			&env.AuthoredAt.WallMS, &env.AuthoredAt.MaxErrorUS, &authState,
			&env.OccurredAt, &env.TimeSource, &payload); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		env.AuthoredAt.State = ClockState(authState)
		env.Payload = json.RawMessage(payload)
		out = append(out, env)
	}
	return out, rows.Err()
}

// EventTimes is Events for a caller that wants only the TIME BASIS of some
// events of one stream (FIX17d: /trips/detail places every trip of a season
// on a day): the wanted seqs' envelopes with the columns the time basis
// reads — site, epoch, seq, AuthoredAt, OccurredAt, TimeSource — and no
// payload, in one scan instead of one point read per event. A seq the log
// does not hold is absent from the result, exactly as Events omits it.
func (s *Store) EventTimes(siteID string, nodeEpoch int64, want map[int64]struct{}) (map[int64]Envelope, error) {
	rows, err := s.conn().Query(
		`SELECT seq, authored_at_wall_ms, authored_at_max_error_us, authored_at_state, occurred_at, time_source
		   FROM event_log WHERE site_id = ? AND node_epoch = ?`, siteID, nodeEpoch)
	if err != nil {
		return nil, fmt.Errorf("store: event times (%s, %d): %w", siteID, nodeEpoch, err)
	}
	defer rows.Close()
	out := make(map[int64]Envelope, len(want))
	for rows.Next() {
		var env Envelope
		var authState string
		if err := rows.Scan(&env.Seq, &env.AuthoredAt.WallMS, &env.AuthoredAt.MaxErrorUS, &authState,
			&env.OccurredAt, &env.TimeSource); err != nil {
			return nil, fmt.Errorf("store: scan event time: %w", err)
		}
		if _, ok := want[env.Seq]; !ok {
			continue
		}
		env.SiteID, env.NodeEpoch = siteID, nodeEpoch
		env.AuthoredAt.State = ClockState(authState)
		out[env.Seq] = env
	}
	return out, rows.Err()
}

// Epochs lists the node_epochs present in the log for a site, ascending.
// C3a's pull server announces its own site's streams with it; C3b's rejoin
// asks the same question about the recovering node's own prior history.
func (s *Store) Epochs(siteID string) ([]int64, error) {
	rows, err := s.conn().Query(
		`SELECT DISTINCT node_epoch FROM event_log WHERE site_id = ? ORDER BY node_epoch`, siteID)
	if err != nil {
		return nil, fmt.Errorf("store: epochs for %s: %w", siteID, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var e int64
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("store: scan epoch: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
