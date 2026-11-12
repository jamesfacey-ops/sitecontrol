// unack.go — F6's correction path for the acknowledgment (window item 17,
// D-42's sweep clause): `alert_ack_retracted`, the `alert_ack_retraction`
// projection, and the predicate change that turns "does ANY ack exist" into
// "does any UNRETRACTED ack exist".
//
// Why this is a separate file from ack.go: ack.go owns the fact, this file
// owns the correction of it. The ack fold and the ack payload are untouched
// (⟦was: "and the two disjoint ack scopes" — the weather-phase scope went
// with weather phases, James 2026-10-04⟧) — F6 §6, "the `alert_ack` table and fold
// are unchanged" — and the whole of the change lives here plus the two
// resolution points in ackedTrips.
//
// THE ALGEBRA, in one line (F6 §3.3): a trip is acked iff there exists an
// `alert_ack` row for it whose OWN log key is named by no
// `alert_ack_retraction`. That is set membership over named targets. It is
// not arithmetic — `count(acks) − count(retractions)` is order-independent
// too and passes a naive reading of the sentence above, but two retractions
// of ONE ack followed by a fresh re-ack must SUPPRESS (the re-ack stands),
// where the arithmetic yields 2 − 2 = 0 and silently no-ops the officer's
// re-acknowledgment forever. And it consults NO ORDER OF ANY KIND: the log
// key is used as IDENTITY, never as precedence, so no comparison between two
// keys is ever made and permuted cross-stream delivery cannot move the
// answer. The likeliest accidental implementation is a key-precedence
// resolver — "the retraction cancels its target only if the retraction's key
// exceeds the target's" — whose live failure is a permanent silent un-ack
// no-op at the lower-named post. There is no such comparison in this file.
//
// ⚠ IDENTITY IS THE FULL THREE-COLUMN KEY (F6 §3.3, A11). `seq` is issued
// per `(site_id, node_epoch)` — see `store/log.go`'s watermarkTx — so a new
// epoch restarts seq at 1 and `(node_epoch, seq)` is routinely shared across
// the two posts, both starting at epoch 1 seq 1. A matcher consulting fewer
// than all three `ack_*` columns silently WIDENS the retracted set. That
// direction only ever produces more alerting, which is why the pin that
// catches it has to be a SUPPRESSION pin.
package deadline

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sitecontrol/internal/store"
)

// evtAlertAckRetracted is the wire spelling. It is hashed into every
// envelope carrying it and it participates in the canonical dump, so it is
// schema-shaped: it rides the window's 9→10 bump (W1 paid it) and adds no
// second increment.
const evtAlertAckRetracted = "alert_ack_retracted"

// AlertAckRetracted is the F6 correction event: it withdraws exactly ONE
// prior acknowledgment, named by that ack EVENT's own log key. It is an
// ordinary authored event on the retracting site's own stream — appended,
// hashed, replicated, folded; nothing is ever deleted (D-1).
//
// There is no `trip_id`: the target key determines it, and duplicating it
// would create a contradictory-payload class (a retraction whose trip_id
// disagrees with its target's) that the fold could not reject
// order-independently, because the target may not have arrived yet.
//
// There is no reason field, in parity with `alert_acknowledged` itself and
// with `occurred_at_corrected`: the correction direction is the safe one —
// it produces more alerting, never less — so no justification gate is
// warranted. Adding one later is schema-shaped.
//
// Re-suppression after a retraction is a NEW `alert_acknowledged`, never a
// retraction of a retraction: no type targets a retraction, so the algebra
// stays one-directional per event and needs no resolution order.
type AlertAckRetracted struct {
	Type         string  `json:"type"`                  // "alert_ack_retracted"
	AckSiteID    string  `json:"ack_site_id"`           // target ack's site_id
	AckNodeEpoch int64   `json:"ack_node_epoch"`        // target ack's node_epoch
	AckSeq       int64   `json:"ack_seq"`               // target ack's seq
	RecordedBy   *string `json:"recorded_by,omitempty"` // post attribution (D-5)
}

// AckKey is one acknowledgment event's own log key — the identity F6 §2
// targets. All three columns are load-bearing; see this file's header.
type AckKey struct {
	SiteID    string
	NodeEpoch int64
	Seq       int64
}

func (k AckKey) String() string {
	return fmt.Sprintf("(%s, epoch %d, seq %d)", k.SiteID, k.NodeEpoch, k.Seq)
}

// ⟦was: AckScope (ScopePlain / ScopePhase) — which of the two disjoint ack
// scopes an un-ack click repudiated. Only the plain scope remains (weather
// phases removed, James 2026-10-04), so the type went with the other.⟧

// alertAckRetractionProjection folds `alert_ack_retracted` (F6 §3.1). It is
// a second projection beside `alert_ack`, which is the established pattern
// rather than a special case: every registered projection's Apply is called
// for every applied event.
//
// ⚠ THE FOLD IS INSERT-ONLY AND DANGLING-TOLERANT, and that IS the
// order-independence. It inserts whether or not the target ack row has
// arrived. A retraction that folds before its target is inert until the ack
// lands, at which point the ack is born-retracted. A from-zero replay
// re-delivers streams in an order this fold must not care about, so it must
// not look up, validate, or join anything at apply time — a fold that
// dropped a dangling retraction would make one delivery order suppress and
// another alert over the same event set.
func alertAckRetractionProjection() store.Projection {
	return store.Projection{
		Name:    "alert_ack_retraction",
		Table:   "alert_ack_retraction",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS alert_ack_retraction (
			site_id        TEXT    NOT NULL,
			node_epoch     INTEGER NOT NULL,
			seq            INTEGER NOT NULL,
			ack_site_id    TEXT    NOT NULL,
			ack_node_epoch INTEGER NOT NULL,
			ack_seq        INTEGER NOT NULL,
			recorded_by    TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;`,
		Apply: applyAlertAckRetracted,
	}
}

func applyAlertAckRetracted(tx *sql.Tx, env store.Envelope) error {
	var p AlertAckRetracted
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("deadline: ack retraction fold: %w", err)
	}
	if p.Type != evtAlertAckRetracted {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO alert_ack_retraction (site_id, node_epoch, seq, ack_site_id, ack_node_epoch, ack_seq, recorded_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.AckSiteID, p.AckNodeEpoch, p.AckSeq, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("deadline: ack retraction fold: %s: %w", AckKey{p.AckSiteID, p.AckNodeEpoch, p.AckSeq}, err)
	}
	return nil
}

// --- reads ---------------------------------------------------------------

// retractionRow is one row of the `alert_ack_retraction` projection, read
// through the sanctioned scoped dump.
type retractionRow struct {
	SiteID       string  `json:"site_id"`
	NodeEpoch    int64   `json:"node_epoch"`
	Seq          int64   `json:"seq"`
	AckSiteID    string  `json:"ack_site_id"`
	AckNodeEpoch int64   `json:"ack_node_epoch"`
	AckSeq       int64   `json:"ack_seq"`
	RecordedBy   *string `json:"recorded_by"`
}

// target is the ack key this retraction names — ALL THREE COLUMNS.
func (r retractionRow) target() AckKey {
	return AckKey{SiteID: r.AckSiteID, NodeEpoch: r.AckNodeEpoch, Seq: r.AckSeq}
}

// retractionsByTarget indexes every retraction in the log by the ack key it
// names. The map key is the whole three-column struct, so "consulting fewer
// than all three columns" is not something a caller can do by accident:
// there is no narrower key to look up with.
func retractionsByTarget(st *store.Store) (map[AckKey][]retractionRow, error) {
	rows, err := dumpSection[retractionRow](st, "alert_ack_retraction")
	if err != nil {
		return nil, err
	}
	out := make(map[AckKey][]retractionRow, len(rows))
	for _, r := range rows {
		k := r.target()
		out[k] = append(out[k], r)
	}
	return out, nil
}

// attribution is the post to name for one ack or one retraction: its
// `recorded_by` where the authoring post recorded one, otherwise the
// site_id the envelope carries structurally (F6 §5: "the effective ack's
// `recorded_by`/`site_id`"). It never renders blank — an attribution that
// renders blank is worse than one that renders an id, which is the same
// choice config.PostName already makes for the same reason.
func attribution(recordedBy *string, siteID string) string {
	if recordedBy != nil && *recordedBy != "" {
		return *recordedBy
	}
	return siteID
}

// EffectiveAcks is the AUTHORING surface's target set (F6 §3.4 rule 1):
// every currently-EFFECTIVE (non-retracted) ack for the trip, in log-key
// order. ⟦was: "in one scope" — and a paragraph on why the PHASE scope was
// wide on purpose (a dormant phase-2 ack at phase 3); weather phases removed,
// James 2026-10-04⟧
//
// ⚠ "EFFECTIVE" IS NOT DECORATION. Enumerating the raw `alert_ack` rows
// instead would author a second retraction naming an already-retracted ack.
// Its live consequence is benign — the set predicate makes the duplicate a
// no-op — but it is one click authoring more events than the officer's act
// contains, and it is the difference the word does all the work of stating.
func EffectiveAcks(st *store.Store, tripID string) ([]AckKey, error) {
	if tripID == "" {
		return nil, fmt.Errorf("deadline: effective acks: trip id is required")
	}
	acks, err := dumpSection[ackRow](st, "alert_ack", store.Eq{Column: "trip_id", Value: tripID})
	if err != nil {
		return nil, err
	}
	retracted, err := retractionsByTarget(st)
	if err != nil {
		return nil, err
	}
	var out []AckKey
	for _, a := range acks {
		if legacyPhaseAck(a) {
			continue // never an ack (P4c): nothing to retract
		}
		k := AckKey{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}
		if len(retracted[k]) > 0 {
			continue // already repudiated by a standing retraction
		}
		out = append(out, k)
	}
	sortAckKeys(out)
	return out, nil
}

// sortAckKeys puts a target set in log-key order. This is presentation and
// determinism only — one click authors the same events in the same order at
// both posts — and is NEVER consulted as precedence: the retractions it
// produces are all fully effective whatever order they land in.
func sortAckKeys(ks []AckKey) {
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].SiteID != ks[j].SiteID {
			return ks[i].SiteID < ks[j].SiteID
		}
		if ks[i].NodeEpoch != ks[j].NodeEpoch {
			return ks[i].NodeEpoch < ks[j].NodeEpoch
		}
		return ks[i].Seq < ks[j].Seq
	})
}

// RetractAcks authors ONE `alert_ack_retracted` per target on this site's
// own stream — one click, N retraction events, exactly N.
//
// ⚠ EXACTLY N, not "at least one". Concurrent cross-post acks mean a trip
// can carry two effective acks, and retracting only one would make the
// un-ack a silent no-op on the officer's own screen: the POST succeeds, the
// banner stays silent, and nothing tells them the act did not take.
//
// The refusal is the caller's, and it is exactly the target set's emptiness
// (F6 §3.4 rule 1); an empty slice here authors nothing and reports it, so a
// caller that skipped the check cannot turn a refusal into a silent success.
func RetractAcks(st *store.Store, siteID string, nodeEpoch int64, targets []AckKey, recordedBy *string, occurredAtMS int64, timeSource string) error {
	if len(targets) == 0 {
		return fmt.Errorf("deadline: retract ack: no acknowledgment to retract")
	}
	// Each retraction is its own authored event, so a failure part-way
	// through leaves the earlier ones standing. That is recoverable rather
	// than corrupt — nothing is ever deleted (D-1), every authored retraction
	// is fully effective on its own, and a second click sweeps whatever is
	// left because the target set is recomputed from the effective acks. The
	// caller reports the failure; it does not report success.
	for _, k := range targets {
		payload, err := json.Marshal(AlertAckRetracted{
			Type:         evtAlertAckRetracted,
			AckSiteID:    k.SiteID,
			AckNodeEpoch: k.NodeEpoch,
			AckSeq:       k.Seq,
			RecordedBy:   recordedBy,
		})
		if err != nil {
			return fmt.Errorf("deadline: retract ack: %w", err)
		}
		if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
			return fmt.Errorf("deadline: retract ack %s: %w", k, err)
		}
	}
	return nil
}

// --- the read-path faces (F6 §5, R1 and R2) ------------------------------

// repudiation is one retraction as the row has to render it: who acked, and
// who took that acknowledgment back.
type repudiation struct {
	ackedBy     string
	retractedBy string
}

// ackFaces carries what one trip's rows owe the officer. It is
// built by ackedTrips from the two dumps the §3.3 predicate already reads —
// no new computation, and no stored status anywhere: this is recomputed on
// every Evaluate like the predicate it rides.
type ackFaces struct {
	// acknowledgedBy: the posts whose EFFECTIVE acks stand, in key order.
	acknowledgedBy []string
	// repudiations: one per retraction naming one of this trip's acks, in
	// the retractions' own key order.
	repudiations []repudiation
}

// R1 — attribution, on an acked row: "acknowledged by Pad Site Control".
//
// Empty when no effective ack stands, which is exactly when the row is not
// an acked row. There is deliberately no nil-receiver arm on either face:
// stampAckFaces is the only caller and it returns on nil, so a nil check
// here would be a branch that can never be taken.
func (f *ackFaces) ackFace() string {
	if len(f.acknowledgedBy) == 0 {
		return ""
	}
	return "acknowledged by " + joinNames(dedupe(f.acknowledgedBy))
}

// R2 — repudiation, owed on ANY row for a trip carrying a retraction,
// WHETHER OR NOT the trip is currently acked: "previously acknowledged by
// Ice Camp B, un-acknowledged by Pad Site Control".
//
// ⚠ THE UN-ACKED ROW IS THE ONE THIS EXISTS FOR. Post A acks an overdue trip
// after radio contact; post B, which did not hear the call, believes it was
// a mis-click and un-acks; the red row returns at both posts — correct, and
// the safe direction. Without this clause the returned row at post A is
// BYTE-IDENTICAL to a fresh overdue alert, so A's officer reads a red row
// for a trip they personally cleared ten minutes ago, concludes the ack did
// not take, and acknowledges again — overruling an officer who had a reason
// to doubt, without ever being shown that anyone doubted. Returned-to-
// unacknowledged must be observably different from never-acknowledged, and
// this string is the whole of that difference.
func (f *ackFaces) repudiationFace() string {
	if len(f.repudiations) == 0 {
		return ""
	}
	var parts []string
	for _, r := range f.repudiations {
		parts = append(parts, fmt.Sprintf("previously acknowledged by %s, un-acknowledged by %s", r.ackedBy, r.retractedBy))
	}
	// Semicolons, not "and": each clause is already a sentence with its own
	// comma, and joining two of them with "and" reads as one clause with
	// three posts in it.
	return strings.Join(dedupe(parts), "; ")
}

// dedupe drops repeats while keeping first-seen order — two posts that both
// acked are two names; one post that acked twice is one.
func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// joinNames renders a list as prose: "A", "A and B", "A, B and C".
func joinNames(in []string) string {
	switch len(in) {
	case 0:
		return ""
	case 1:
		return in[0]
	}
	head := in[:len(in)-1]
	out := ""
	for i, s := range head {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out + " and " + in[len(in)-1]
}
