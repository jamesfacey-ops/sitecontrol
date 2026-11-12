// time_correct.go — item 14: the `occurred_at_corrected` event, its
// INSERT-only `occurred_at_correction` side table, the authoring edge, and
// the VF §4.2 resolver `ResolveOccurredAt` (W4; design/WINDOW-BUILD-PLAN-v1
// "### W4", criteria LOCKED 2026-08-27; design/time-provenance-v1.md rev 3
// §4.7; design/visible-failure-v1.md rev 2 §4).
//
// The load-bearing rules, in one place:
//
//   - A correction is a NEW event; the target `event_log` row is NEVER
//     edited (D-42 obligation 1, W4-1). The fold appends a side-table row
//     and touches nothing else; after N corrections the first-entered value
//     and every intermediate remain queryable at rest (W4-2).
//   - The correction's own envelope describes the correction ACT (D-44):
//     the edge authors with time_source "default", so rule W stamps
//     OccurredAt := AuthoredAt from one clock reading. The corrected
//     instant lives in the PAYLOAD ONLY — one instant, one home; storing
//     it twice is the two-stores shape this window deletes. D-44 superseded
//     D-42 obligation 3 on the storage question only: `entered` appears on
//     the corrected value BY CONSTRUCTION at classification (TP §7.4's
//     substitution cell), never on the correction event's own envelope.
//   - The payload is EXACTLY {target key triple, corrected instant,
//     recorded_by} — no reason field, no note field (operative default #15;
//     the five key literals sit on B-2's wire-freeze list).
//   - Resolution is CAUSAL, then latest-per-target in RECORD order —
//     ascending (site_id, node_epoch, seq), epoch-major; the clock decides
//     nothing (W4-3 as RE-LOCKED at SchemaVersion 11 under [[D-92]]). Every
//     correction carries `seen`: the corrections in the ORIGINAL's chain
//     its author had already seen standing. A correction named by
//     another's `seen` is DOMINATED and never wins; among the undominated
//     the greatest key wins — so a correction made in knowledge of an
//     earlier one wins regardless of site_id, and site_id breaks ties ONLY
//     between genuinely concurrent corrections. ⟦was: "Resolution is
//     latest-per-target in RECORD order … (D-28 B-i, W4-3)" — D-28 Part B
//     is REVERSED by D-92; re-ruled, not silently edited⟧. The rule is
//     resolveContest (person_amend.go), read here through contest.go and
//     never respelled. What is NOT the causal reference: the target key.
//     W4-11's one-hop rule stands — target references LOCATE the original
//     and its chain, nothing more — because a correction form is opened on
//     whatever key the officer navigated from, and that is a UI accident,
//     not knowledge. The SQL ordering spelling is SHARED with the
//     amendment composition (the ONE spelling, W3-11); the Go half is
//     store.Key.Less, and the T-A oracle proves the two agree.
//   - A correction of a correction competes in the ORIGINAL target's chain
//     (TP §4.7's one-hop rule, W4-11): "the chain for a target" is every
//     correction reachable by following target references to that original.
//   - A cycle while following target references, and a target this store
//     does not hold (partial replication), both resolve FAIL-CLOSED: a
//     non-nil error, never a hang, never a silent drop, never a
//     correction's value presented as effective (TP §4.7, W4-9/W4-10).
//     Status is never stored, so the dangling case self-heals with zero
//     machinery the moment the target replicates in (VF §4.3 rule 2).
//   - Supersession is NEVER stored: the side table carries no status-shaped
//     column and is INSERT-only by trigger (VF §4.3 rule 2 / T11; W4-1's
//     structural half).
//   - Reads are TARGET-SCOPED (the W3 critique's M-1 rule): a per-target
//     resolution queries by target key, never a whole-table decode.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"sitecontrol/internal/store"
)

// --- event payload -------------------------------------------------------

// OccurredAtCorrected is the wire payload (type "occurred_at_corrected").
// The five value keys are pinned literals on B-2's wire-freeze list —
// nothing else, no reason field (operative default #15). No omitempty on
// any of the five: every authored correction carries all five keys.
//
// ⟦SchemaVersion 11⟧ `seen` is ADDED ([[D-92]]), the seventh payload to
// carry it: the keys of the corrections in the ORIGINAL's chain this author
// had already seen standing — the chain's undominated heads as the
// authoring post resolved it. It is what lets a correction made in
// knowledge of an earlier one WIN regardless of site_id (ResolveOccurredAt);
// stamped by CorrectOccurredAt, never by a caller. It follows D-92's wire
// convention rather than this carrier's five-key one: OMITTED when the
// author saw no correction standing, so the FIRST correction of an event is
// byte-identical to its 10-shape and W4-6's exact-key pin still holds for
// it; a later one carries the sixth key. ⟦was: "No omitempty anywhere:
// every authored correction carries all five keys, so the hashed byte shape
// is one shape" — two shapes now, the second only over a standing
// correction⟧
type OccurredAtCorrected struct {
	Type                string    `json:"type"` // "occurred_at_corrected"
	TargetSiteID        string    `json:"target_site_id"`
	TargetNodeEpoch     int64     `json:"target_node_epoch"`
	TargetSeq           int64     `json:"target_seq"`
	CorrectedOccurredAt int64     `json:"corrected_occurred_at"` // int64 UTC ms, §4.1 conversion
	RecordedBy          string    `json:"recorded_by"`
	Seen                []SeenRef `json:"seen,omitempty"`
}

const evtOccurredAtCorrected = "occurred_at_corrected"

// factOccurredAt is the carrier's one-fact vocabulary (VF §4.2's own
// naming; corrections are the degenerate single-fact case).
const factOccurredAt = "occurred_at"

// TimeCorrection is the authoring-edge input (SEAM W4-S-EDGE). There is
// DELIBERATELY no (occurredAtMS, timeSource) trailing pair on the edge:
// overriding the correction act's own time is declined UI (D-44; VF §5),
// so the edge offers no parameter for it — the act's envelope pair is
// always default-captured (rule W).
type TimeCorrection struct {
	Target                store.Key
	CorrectedOccurredAtMS int64
	RecordedBy            string
}

// CorrectOccurredAt authors one occurred_at_corrected event. The POST
// appends — a losing correction is recorded and superseded, never refused
// (VF §5) — so the only refusal here is a structurally unusable target key.
// The target is NOT required to exist locally: next-seq is predictable and
// a not-yet-replicated target is a legal, transient state the read path
// resolves fail-closed until it heals (TP §4.7's dangling rule).
func CorrectOccurredAt(st *store.Store, siteID string, nodeEpoch int64, in TimeCorrection) (store.Envelope, error) {
	if in.Target.SiteID == "" || in.Target.NodeEpoch < 1 || in.Target.Seq < 1 {
		return store.Envelope{}, fmt.Errorf("domain: correct occurred-at: refused — %v is not a well-formed event key; nothing was recorded", in.Target)
	}
	// The `seen` stamp ([[D-92]], SchemaVersion 11): the undominated heads
	// of the ORIGINAL's correction chain as THIS post resolves it right now
	// — the state the author actually saw. The chain is located exactly as
	// ResolveOccurredAt locates it (W4-11: target references find the
	// original; they are NOT the causal reference — see the file comment).
	// A target this post does not hold yet is legal (next-seq is
	// predictable): the stamp then names whatever corrections of it are
	// present, which may be none. A chain this post cannot resolve (a
	// wire-formed target cycle, an unreadable target row) stamps nothing
	// rather than refusing: the POST appends (VF §5), and the read stays
	// fail-closed on that chain until it heals. A read failure of the chain
	// itself is an internal fault and is returned as one.
	var seen []SeenRef
	if original, oerr := correctionOriginal(st, in.Target); oerr == nil {
		chain, cerr := correctionChainForOriginal(st, original)
		if cerr != nil {
			return store.Envelope{}, fmt.Errorf("domain: correct occurred-at: %w", cerr)
		}
		_, heads := resolveContest(chain)
		seen = seenRefsOf(heads)
	}
	payload, err := json.Marshal(OccurredAtCorrected{
		Type:                evtOccurredAtCorrected,
		TargetSiteID:        in.Target.SiteID,
		TargetNodeEpoch:     in.Target.NodeEpoch,
		TargetSeq:           in.Target.Seq,
		CorrectedOccurredAt: in.CorrectedOccurredAtMS,
		RecordedBy:          in.RecordedBy,
		Seen:                seen,
	})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: correct occurred-at: %w", err)
	}
	// D-44: the act's envelope pair is default-captured — rule W copies
	// OccurredAt from the same single clock reading; the corrected instant
	// rides the payload ONLY.
	env, err := st.Author(siteID, nodeEpoch, payload, 0, "default")
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: correct occurred-at: %w", err)
	}
	return env, nil
}

// --- the INSERT-only side table (projection) -----------------------------

// occurredAtCorrectionProjection registers the side table the fold
// maintains and the resolver reads (VF §7.5 obligation 1 / W4 Changes
// delta 1). Keyed by the correction event's OWN key. The declared column
// set is EXACT (W4-1's structural half / SEAM W4-ST-COLS): the key triple,
// the target key triple, the corrected instant, recorded_by, and — since
// SchemaVersion 11 ([[D-92]]) — `seen` (the author's seen-list, JSON, NULL
// when empty: a reference written at authoring, never a status) — and NO
// status-shaped column, ever: supersession is computed at read, never
// stored (VF §4.3 rule 2); the two triggers make INSERT-only a mechanism
// rather than a convention.
func occurredAtCorrectionProjection() store.Projection {
	return store.Projection{
		Name:    "occurred_at_correction",
		Table:   "occurred_at_correction",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS occurred_at_correction (
			site_id               TEXT NOT NULL,
			node_epoch            INTEGER NOT NULL,
			seq                   INTEGER NOT NULL,
			target_site_id        TEXT,
			target_node_epoch     INTEGER,
			target_seq            INTEGER,
			corrected_occurred_at INTEGER,
			recorded_by           TEXT,
			seen                  TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE TRIGGER IF NOT EXISTS occurred_at_correction_insert_only_update
			BEFORE UPDATE ON occurred_at_correction BEGIN
			SELECT RAISE(ABORT, 'occurred_at_correction is INSERT-only: supersession is computed at read, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS occurred_at_correction_insert_only_delete
			BEFORE DELETE ON occurred_at_correction BEGIN
			SELECT RAISE(ABORT, 'occurred_at_correction is INSERT-only: corrections are superseded, never merely absent');
		END;`,
		Apply: applyOccurredAtCorrected,
	}
}

// applyOccurredAtCorrected is the fold. IT NEVER REFUSES and it NEVER
// touches the target row (D-27-style: a new event, never an edit — W4-1).
// Values are coerced tolerantly, field by field: a wire payload this
// decoder finds odd must still land (the read path fails closed on the
// junk; the fold does not fork the logs over it).
func applyOccurredAtCorrected(tx *sql.Tx, env store.Envelope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		return nil // not an object-shaped payload; not this fold's event
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != evtOccurredAtCorrected {
		return nil
	}
	asText := func(key string) any {
		r, ok := raw[key]
		if !ok {
			return nil
		}
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			return s
		}
		if string(r) == "null" {
			return nil
		}
		return string(r) // tolerant: a non-string value lands as its JSON text
	}
	asInt := func(key string) any {
		r, ok := raw[key]
		if !ok {
			return nil
		}
		var n int64
		if err := json.Unmarshal(r, &n); err == nil {
			return n
		}
		return nil // a non-integer lands as NULL; the resolver fails closed on it
	}
	// `seen` ([[D-92]]) through the one codec, seenColumnValue (contest.go):
	// absent, empty or malformed lands as NULL — the fold never refuses.
	seen, err := seenColumnValue(raw)
	if err != nil {
		return fmt.Errorf("domain: occurred_at_corrected fold: seen: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO occurred_at_correction
		(site_id, node_epoch, seq, target_site_id, target_node_epoch, target_seq,
		 corrected_occurred_at, recorded_by, seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq,
		asText("target_site_id"), asInt("target_node_epoch"), asInt("target_seq"),
		asInt("corrected_occurred_at"), asText("recorded_by"), seen)
	if err != nil {
		return fmt.Errorf("domain: occurred_at_corrected fold: insert %v: %w", env.Key(), err)
	}
	return nil
}

// --- reading the side table (target-scoped, never a whole-table decode) --

// occurredAtCorrectionRow is one decoded side-table row: the correction's
// own key, its target, its asserted instant and its officer. The OK flags
// carry the tolerant fold's honesty: a wire row whose target or instant
// did not decode is present (the record set keeps it) and unusable (the
// resolver fails closed on it rather than inventing a value).
type occurredAtCorrectionRow struct {
	Key         store.Key
	Target      store.Key
	TargetOK    bool
	CorrectedMS int64
	CorrectedOK bool
	RecordedBy  string
	// Seen (SchemaVersion 11, [[D-92]]) is the keys of the corrections in
	// the original's chain this row's author had already seen standing —
	// the dominance input to resolveContest. Empty: competes on key alone.
	Seen []store.Key
}

// The contestant surface (contest.go). A correction is a contestant, not an
// amendmentRow: its contest is W4-11's chain, not a target-id scope.
func (r occurredAtCorrectionRow) contestKey() store.Key    { return r.Key }
func (r occurredAtCorrectionRow) contestSeen() []store.Key { return r.Seen }

// The correction reads share the amendment composition's ONE ordering
// spelling (personAmendmentCompositionOrderBy — W3-11: one SQL spelling
// beside the one Go comparator, never respelled). Both reads are scoped by
// a WHERE (M-1's rule): the point-read by the correction's own key, and
// the per-target read the chain walk expands from.
const (
	occurredAtCorrectionSelect = `SELECT site_id, node_epoch, seq,
	target_site_id, target_node_epoch, target_seq,
	corrected_occurred_at, recorded_by, seen
	FROM occurred_at_correction`

	occurredAtCorrectionByOwnKeySQL = occurredAtCorrectionSelect +
		` WHERE site_id = ? AND node_epoch = ? AND seq = ?`

	occurredAtCorrectionByTargetSQL = occurredAtCorrectionSelect +
		` WHERE target_site_id = ? AND target_node_epoch = ? AND target_seq = ?` +
		personAmendmentCompositionOrderBy
)

// queryCorrections is the shared decode loop under both scoped reads.
func queryCorrections(st *store.Store, query string, args ...any) ([]occurredAtCorrectionRow, error) {
	var out []occurredAtCorrectionRow
	err := st.LocalRead(func(tx *sql.Tx) error {
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				k            store.Key
				tSite        sql.NullString
				tEpoch, tSeq sql.NullInt64
				ms           sql.NullInt64
				by, seen     sql.NullString
			)
			if err := rows.Scan(&k.SiteID, &k.NodeEpoch, &k.Seq, &tSite, &tEpoch, &tSeq, &ms, &by, &seen); err != nil {
				return err
			}
			r := occurredAtCorrectionRow{Key: k, RecordedBy: by.String, Seen: decodeSeenColumn(seen)}
			if tSite.Valid && tEpoch.Valid && tSeq.Valid {
				r.Target = store.Key{SiteID: tSite.String, NodeEpoch: tEpoch.Int64, Seq: tSeq.Int64}
				r.TargetOK = true
			}
			if ms.Valid {
				r.CorrectedMS = ms.Int64
				r.CorrectedOK = true
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("domain: read corrections: %w", err)
	}
	return out, nil
}

// correctionRowAt point-reads the side table by a correction's OWN key;
// nil = the key is not a correction event.
func correctionRowAt(st *store.Store, k store.Key) (*occurredAtCorrectionRow, error) {
	rows, err := queryCorrections(st, occurredAtCorrectionByOwnKeySQL, k.SiteID, k.NodeEpoch, k.Seq)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// correctionsTargeting reads the corrections that directly target one key,
// in ascending key order.
func correctionsTargeting(st *store.Store, target store.Key) ([]occurredAtCorrectionRow, error) {
	return queryCorrections(st, occurredAtCorrectionByTargetSQL, target.SiteID, target.NodeEpoch, target.Seq)
}

// eventAt is the §4.1 sanctioned point-read: one event_log row by its
// primary key. Not an audit read over the log — a keyed lookup.
func eventAt(st *store.Store, k store.Key) (store.Envelope, bool, error) {
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		return store.Envelope{}, false, err
	}
	if len(evs) == 0 || evs[0].Seq != k.Seq {
		return store.Envelope{}, false, nil
	}
	return evs[0], true, nil
}

// correctionOriginal follows target references from k down to the ORIGINAL
// — the first key that is not itself a correction event (TP §4.7's
// one-hop rule generalised to any depth of correction-of-correction). A
// key revisited on the walk is a cycle, which resolves EXACTLY as a
// dangling target: fail-closed, a non-nil error, and the walk TERMINATES —
// the visited set bounds it to one step per side-table row, so a chain
// follower with no cycle detection cannot be built out of this function
// (TP §4.7's cycle guard; the loop is constructible in normal operation
// because next-seq is predictable).
func correctionOriginal(st *store.Store, k store.Key) (store.Key, error) {
	reg := carrierRegistry[evtOccurredAtCorrected]
	visited := map[store.Key]bool{}
	cur := k
	for {
		if visited[cur] {
			return store.Key{}, fmt.Errorf("domain: resolve occurred-at: correction target references form a cycle through %v — resolved exactly as a dangling target: fail-closed, no correction's value effective (TP §4.7)", cur)
		}
		visited[cur] = true
		row, err := correctionRowAt(st, cur)
		if err != nil {
			return store.Key{}, err
		}
		if row == nil {
			return cur, nil // not a correction: the original
		}
		if !row.TargetOK {
			return store.Key{}, fmt.Errorf("domain: resolve occurred-at: correction %v carries no readable target key — fail-closed, never a guess", cur)
		}
		cur = reg.CorrectionTarget(*row)
	}
}

// correctionChainForOriginal gathers every correction reachable by
// following target references to the original — the reverse walk, expanded
// frontier by frontier with target-scoped queries (never a whole-table
// decode) — and returns it in ascending key order (store.Key.Less, the one
// Go comparator; each per-frontier SQL read carries the one ORDER BY
// spelling, and the cross-frontier merge re-establishes the same total
// order).
func correctionChainForOriginal(st *store.Store, original store.Key) ([]occurredAtCorrectionRow, error) {
	var rows []occurredAtCorrectionRow
	seen := map[store.Key]bool{original: true}
	frontier := []store.Key{original}
	for len(frontier) > 0 {
		var next []store.Key
		for _, tk := range frontier {
			batch, err := correctionsTargeting(st, tk)
			if err != nil {
				return nil, err
			}
			for _, r := range batch {
				if seen[r.Key] {
					continue
				}
				seen[r.Key] = true
				rows = append(rows, r)
				next = append(next, r.Key)
			}
		}
		frontier = next
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key.Less(rows[j].Key) })
	return rows, nil
}

// --- the resolver (VF §4.2, verbatim signatures) -------------------------

// ResolvedTime is the effective occurred instant for one target
// (visible-failure-v1 §4.2, verbatim — no field added, no field renamed).
type ResolvedTime struct {
	InstantUTCms int64
	Corrected    bool      // false → envelope value, envelope provenance
	Winner       store.Key // the winning correction when Corrected, the
	// TARGET ITSELF when not (Winner == target iff the envelope value
	// stands — the FactOutcome.Winner convention).
	RecordedBy string // the winning correction's recorded_by; empty when
	// Corrected == false (the envelope has no officer). Renderers gate on
	// non-empty, never assume.
}

// Correction is one chain entry (visible-failure-v1 §4.2, verbatim).
type Correction struct {
	Key          store.Key
	InstantUTCms int64
	RecordedBy   string
	Status       Status
}

// ResolveOccurredAt is the surface-facing direction of the outcome
// relation: the effective occurred instant for target, its provenance, and
// the full correction chain in ascending key order, each entry carrying
// its Status (visible-failure-v1 §4.2). An empty chain means the target's
// own envelope pair stands uncorrected, read via the §4.1 point-read —
// entry zero is never in the chain and never in any side table.
//
// target may itself be a correction event's key: it is resolved through
// its target references to the ORIGINAL first (TP §4.7's one-hop rule), so
// the returned chain is always the original's.
//
// ERRORS, not empties, fail-closed (TP §4.7; VF §4.2): a target matching
// no local event (partial replication), a cycle among target references,
// or a correction whose stored instant is unreadable all return a non-nil
// error — never a silent drop, never a correction's value presented as
// effective. Nothing is stored: the dangling case self-heals at the first
// read after the target replicates in (VF §4.3 rule 2).
func ResolveOccurredAt(st *store.Store, target store.Key) (ResolvedTime, []Correction, error) {
	original, err := correctionOriginal(st, target)
	if err != nil {
		return ResolvedTime{}, nil, err
	}
	env, ok, err := eventAt(st, original)
	if err != nil {
		return ResolvedTime{}, nil, fmt.Errorf("domain: resolve occurred-at: %w", err)
	}
	if !ok {
		return ResolvedTime{}, nil, fmt.Errorf("domain: resolve occurred-at: target %v matches no local event — fail-closed until it replicates in (TP §4.7's dangling rule; nothing is stored, so this read heals itself when the target arrives)", original)
	}
	rows, err := correctionChainForOriginal(st, original)
	if err != nil {
		return ResolvedTime{}, nil, err
	}
	if len(rows) == 0 {
		// Uncorrected: the target's own envelope assertion stands.
		return ResolvedTime{InstantUTCms: env.OccurredAt, Corrected: false, Winner: original}, nil, nil
	}
	// ⟦SchemaVersion 11, [[D-92]]⟧ The winner is resolveContest's over the
	// whole chain — the ONE rule (person_amend.go): a correction named by
	// another's `seen` is dominated and never wins; among the undominated
	// the greatest key wins. The chain is still returned in ascending key
	// order (VF §4.2), and Effective sits on the winner wherever it sits.
	// ⟦was: "status = Effective iff i == len(rows)-1; winner :=
	// rows[len(rows)-1]" — latest per target in RECORD order, W4-3, now the
	// tiebreak among the undominated⟧
	winnerKey, _ := resolveContest(rows)
	chain := make([]Correction, len(rows))
	var winner occurredAtCorrectionRow
	for i, r := range rows {
		if !r.CorrectedOK {
			return ResolvedTime{}, nil, fmt.Errorf("domain: resolve occurred-at: correction %v carries no readable corrected instant — fail-closed, never a guess", r.Key)
		}
		status := Superseded
		if r.Key == winnerKey {
			status = Effective
			winner = r
		}
		chain[i] = Correction{Key: r.Key, InstantUTCms: r.CorrectedMS, RecordedBy: r.RecordedBy, Status: status}
	}
	return ResolvedTime{
		InstantUTCms: winner.CorrectedMS,
		Corrected:    true,
		Winner:       winner.Key,
		RecordedBy:   winner.RecordedBy,
	}, chain, nil
}

// correctionOutcome is Outcome's correction-carrier branch: the per-fact
// answer for one occurred_at_corrected event, computed by resolving the
// ORIGINAL target's chain (so a correction-of-correction competes there,
// never in a sub-chain) and asking where this event landed in it.
func correctionOutcome(st *store.Store, k store.Key, row occurredAtCorrectionRow) ([]FactOutcome, error) {
	reg := carrierRegistry[evtOccurredAtCorrected]
	eff, _, err := ResolveOccurredAt(st, k)
	if err != nil {
		return nil, fmt.Errorf("domain: outcome: %w", err)
	}
	status := Superseded
	winner := eff.Winner
	if eff.Winner == k {
		status = Effective
		winner = k
	}
	var out []FactOutcome
	for _, fact := range reg.Facts {
		out = append(out, FactOutcome{
			Fact:     fact,
			Asserted: reg.AssertedCorrection(row, fact),
			Status:   status,
			Winner:   winner,
		})
	}
	return out, nil
}

// renderCorrectedInstant is the carrier's canonical Asserted rendering
// (VF §4.4's second registration good): the claimed instant, UTC, RFC3339.
func renderCorrectedInstant(row occurredAtCorrectionRow, fact string) string {
	if fact != factOccurredAt || !row.CorrectedOK {
		return ""
	}
	return time.UnixMilli(row.CorrectedMS).UTC().Format(time.RFC3339)
}
