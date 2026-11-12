// ack.go — the explicit, LOGGED acknowledgment (§9.4/A14): "a persistent
// on-screen banner until arrival check-in or an explicit, logged
// acknowledgment." The acknowledgment is an ordinary authored event on the
// acknowledging site's own stream — it lands in the event log, replicates
// like everything else (§8.1: one log, one mechanism), and folds into the
// alert_ack projection below. This is why store.SchemaVersion bumps at C7:
// a binary that does not register this fold would compute a different dump
// over the same log, which is exactly what the §8.5 freeze exists to refuse.
package deadline

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"sitecontrol/internal/store"
)

// AlertAcknowledged is the event payload. TripID references the trip whose
// alert (live or overdue-unresolved) the officer explicitly dismissed.
//
// ⟦was: ReviewPhase (`review_phase`, SchemaVersion 8) and AckedAtBoard
// (`acked_at_board`, SchemaVersion 9, ruling F38) — the phase-scoped ack of a
// weather VERIFY row and its declaration episode. Removed with weather
// phases, James 2026-10-04, inside the unreleased v16. A legacy ack still
// carrying `review_phase` is SKIPPED by the fold (applyAlertAcknowledged): it
// answered a weather row that no longer exists, and reading it as a plain
// ack would silence the trip's red — an under-alert through a side door.⟧
type AlertAcknowledged struct {
	Type       string  `json:"type"` // "alert_acknowledged"
	TripID     string  `json:"trip_id"`
	RecordedBy *string `json:"recorded_by,omitempty"`
	// AlertKind (SchemaVersion 16) — James, 2026-10-04 (ruling R3): "an ack
	// on an amber VERIFY row should not silence the other end's red overdue;
	// ack per alert kind." The kind of row the officer pressed on, stamped
	// by the server from its own board: "overdue_live" (red) or
	// "overdue_unresolved" (amber VERIFY BY RADIO). A red ack silences red
	// and amber at both ends; an amber ack silences amber only, so a red row
	// is never silenced by an amber ack. Nil (a pre-16 ack, or a caller
	// that does not know) keeps the old meaning: it
	// silences both kinds. The kind is not derivable from the event set —
	// amber vs red at a post depends on that post's link state at the press.
	AlertKind *string `json:"alert_kind,omitempty"`
}

// The two alert kinds an ordinary ack can carry (AlertAcknowledged.AlertKind).
const (
	AckKindRed   = string(StatusOverdueLive)
	AckKindAmber = string(StatusOverdueUnresolved)
)

// alertAckProjection folds alert_acknowledged. Rows are keyed by the ack
// EVENT's own source key — set-based and trivially order-independent across
// streams (every delivered ack inserts its own unique row; readers ask "does
// ANY ack exist for this trip", never "which came first"). Keying by trip_id
// alone would make the surviving recorded_by delivery-order-dependent.
func alertAckProjection() store.Projection {
	return store.Projection{
		Name:    "alert_ack",
		Table:   "alert_ack",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS alert_ack (
			site_id      TEXT    NOT NULL,
			node_epoch   INTEGER NOT NULL,
			seq          INTEGER NOT NULL,
			trip_id      TEXT    NOT NULL,
			recorded_by  TEXT,
			alert_kind   TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;`,
		Apply: applyAlertAcknowledged,
	}
}

func applyAlertAcknowledged(tx *sql.Tx, env store.Envelope) error {
	var p AlertAcknowledged
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("deadline: ack fold: %w", err)
	}
	if p.Type != "alert_acknowledged" {
		return nil
	}
	// A legacy weather-phase ack (a v16 log written before weather phases
	// were removed, James 2026-10-04) is SKIPPED — never refused, so no
	// stored log can stop a post opening, and never folded as a plain ack,
	// which would silence the trip's red. Presence of the key decides it,
	// whatever its value.
	var legacy struct {
		ReviewPhase json.RawMessage `json:"review_phase"`
	}
	if err := json.Unmarshal(env.Payload, &legacy); err == nil && len(legacy.ReviewPhase) > 0 && string(legacy.ReviewPhase) != "null" {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO alert_ack (site_id, node_epoch, seq, trip_id, recorded_by, alert_kind)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.TripID, p.RecordedBy, p.AlertKind,
	); err != nil {
		return fmt.Errorf("deadline: ack fold: %s: %w", p.TripID, err)
	}
	return nil
}

// Register wires C7's replicated projection into st — called at the
// composition root right after domain.Register (same convention; deadline
// cannot live inside domain.Register because domain must not import this
// package, and the fold belongs to the chunk that owns the event).
//
// F6/W9c adds the SECOND projection here: `alert_ack_retraction`, the
// correction path for the acknowledgment (unack.go). The two are registered
// together because the §3.3 predicate reads both on every scan — a store
// with one and not the other would answer "is this trip acked" from half the
// evidence.
func Register(st *store.Store) error {
	if err := st.RegisterProjection(alertAckProjection()); err != nil {
		return err
	}
	return st.RegisterProjection(alertAckRetractionProjection())
}

// Acknowledge authors the ordinary per-trip acknowledgment on this site's
// own stream. Authoring-time refusals check locally-known state (the
// CreateTrade pattern): the trip must be known here. Whether it is
// CURRENTLY alerting is the caller's check (web asks the scan) — this
// function stays UI-free.
//
// It authors a kind-less ack (silences both kinds — the pre-16 meaning).
// The web ack door uses AcknowledgeKind, which records the kind of row the
// officer pressed on (ruling R3).
func Acknowledge(st *store.Store, siteID string, nodeEpoch int64, tripID string, recordedBy *string, occurredAtMS int64, timeSource string) error {
	return acknowledge(st, siteID, nodeEpoch, tripID, nil, recordedBy, occurredAtMS, timeSource)
}

// AcknowledgeKind is Acknowledge recording the KIND of row acknowledged
// (James, 2026-10-04, ruling R3): AckKindRed for a live overdue alert,
// AckKindAmber for a VERIFY BY RADIO (overdue-unresolved) row. Any other
// kind is refused and authors nothing.
func AcknowledgeKind(st *store.Store, siteID string, nodeEpoch int64, tripID string, kind string, recordedBy *string, occurredAtMS int64, timeSource string) error {
	if kind != AckKindRed && kind != AckKindAmber {
		return fmt.Errorf("deadline: acknowledge: alert kind %q is neither %q nor %q — only an overdue or verify-by-radio row can be acknowledged", kind, AckKindRed, AckKindAmber)
	}
	return acknowledge(st, siteID, nodeEpoch, tripID, &kind, recordedBy, occurredAtMS, timeSource)
}

// ⟦was: AcknowledgePhaseReview — the phase-scoped ack of a weather VERIFY
// row; removed with weather phases, James 2026-10-04⟧

func acknowledge(st *store.Store, siteID string, nodeEpoch int64, tripID string, alertKind *string, recordedBy *string, occurredAtMS int64, timeSource string) error {
	if tripID == "" {
		return fmt.Errorf("deadline: acknowledge: trip id is required")
	}
	known, err := tripKnown(st, tripID)
	if err != nil {
		return fmt.Errorf("deadline: acknowledge: %w", err)
	}
	if !known {
		return fmt.Errorf("deadline: acknowledge: trip %s is not known at this site", tripID)
	}
	payload, err := json.Marshal(AlertAcknowledged{Type: "alert_acknowledged", TripID: tripID, RecordedBy: recordedBy, AlertKind: alertKind})
	if err != nil {
		return fmt.Errorf("deadline: acknowledge: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return fmt.Errorf("deadline: acknowledge: %w", err)
	}
	return nil
}

// --- reads (the dump is the one sanctioned read path outside a fold tx) ---

type ackRow struct {
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	TripID     string  `json:"trip_id"`
	RecordedBy *string `json:"recorded_by"`
	AlertKind  *string `json:"alert_kind"`
	// ReviewPhase is present only in a store folded by a pre-removal v16
	// build, whose alert_ack table still has the column (CREATE TABLE IF NOT
	// EXISTS kept it; DumpProjection is SELECT *). See legacyPhaseAck.
	ReviewPhase json.RawMessage `json:"review_phase,omitempty"`
}

// legacyPhaseAck reports a row folded from a weather-phase ack BEFORE weather
// phases were removed (James, 2026-10-04). The fold now skips such an ack
// (applyAlertAcknowledged), but a store folded by the earlier build is not
// re-folded on open and still holds the row — with alert_kind NULL, which
// reads as a kind-less ack and silences the trip's red (R6 L06 P4c). Every
// reader skips it, so the in-place store agrees with a fresh fold of the
// same log.
func legacyPhaseAck(r ackRow) bool {
	return len(r.ReviewPhase) > 0 && string(r.ReviewPhase) != "null"
}

type tripRow struct {
	TripID string `json:"trip_id"`
}

// dumpSection mirrors domain's helper of the same name (unexported there, and
// exporting it for one consumer would widen domain's surface for no gain). It
// reads one table via the store's scoped dump — never the whole-database
// snapshot (F30).
func dumpSection[T any](st *store.Store, name string, conds ...store.Eq) ([]T, error) {
	b, err := st.DumpProjectionWhere(name, conds...)
	if err != nil {
		return nil, fmt.Errorf("deadline: dump: %w", err)
	}
	var rows []T
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("deadline: decode projection %q: %w", name, err)
	}
	return rows, nil
}

// ackSets is the trip's effective acks: trip[id] = an EFFECTIVE red or
// kind-less ack exists (suppression is ∃-over-the-set — order-free).
// ⟦was: "separates the two disjoint ack scopes … phase[id] = every EFFECTIVE
// phase-scoped ack" — the phase scope went with weather phases, James
// 2026-10-04⟧
//
// ⟦W9c, item 17⟧ "Effective" is F6 §3.3's word: an ack whose own log key is
// named by no `alert_ack_retraction`. tripFaces carries the §5
// R1/R2 rendering inputs, built here because this is the one place that
// already reads both projections — R2 costs no new computation, it reads a
// set the predicate has already materialised.
type ackSets struct {
	trip map[string]bool
	// tripAmberOnly (R3, SchemaVersion 16): an effective ordinary ack exists
	// but every one of them is an AMBER ack — it silences the trip's row
	// only while that row is amber (see silencesPlain). trip[id] is set
	// only by red or kind-less acks.
	tripAmberOnly map[string]bool
	// tripRedSites (FIX14): the sites whose EFFECTIVE red or kind-less ack
	// sets trip[id], in key order, deduplicated — read by OverdueAckedBy.
	tripRedSites map[string][]string

	tripFaces map[string]*ackFaces
}

// facesFor returns the face accumulator for one trip, creating it on first
// use. ⟦was: "in one scope" — the phase scope went with weather phases⟧
func (s ackSets) facesFor(tripID string) *ackFaces {
	f := s.tripFaces[tripID]
	if f == nil {
		f = &ackFaces{}
		s.tripFaces[tripID] = f
	}
	return f
}

// ackedTrips reports the logged acknowledgments.
//
// ⟦W9c, item 17⟧ THE PREDICATE (F6 §3.3): a trip is acked iff there exists
// an `alert_ack` row for it such that no
// `alert_ack_retraction` row names that row's OWN key. The skip below is the
// whole of it. Everything downstream — scan.go's `Acked` stamp and
// `Banner()` — is UNCHANGED: they consume this
// function's output and simply receive a set that no longer contains
// repudiated acks. ⟦was: "`Banner()`, `phaseAckSuppresses`, the F39 guard" —
// the last two went with weather phases⟧
func ackedTrips(st *store.Store) (ackSets, error) {
	out := ackSets{
		trip: map[string]bool{}, tripAmberOnly: map[string]bool{},
		tripRedSites: map[string][]string{}, tripFaces: map[string]*ackFaces{},
	}
	rows, err := dumpSection[ackRow](st, "alert_ack")
	if err != nil {
		return out, err
	}
	retracted, err := retractionsByTarget(st)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		if legacyPhaseAck(r) {
			continue
		}
		faces := out.facesFor(r.TripID)
		// The match is the FULL THREE-COLUMN key. There is no narrower
		// lookup available: AckKey is the map key.
		if rs := retracted[AckKey{SiteID: r.SiteID, NodeEpoch: r.NodeEpoch, Seq: r.Seq}]; len(rs) > 0 {
			for _, rr := range rs {
				faces.repudiations = append(faces.repudiations, repudiation{
					ackedBy:     attribution(r.RecordedBy, r.SiteID),
					retractedBy: attribution(rr.RecordedBy, rr.SiteID),
				})
			}
			continue
		}
		faces.acknowledgedBy = append(faces.acknowledgedBy, attribution(r.RecordedBy, r.SiteID))
		if r.AlertKind != nil && *r.AlertKind == AckKindAmber {
			out.tripAmberOnly[r.TripID] = true
		} else {
			out.trip[r.TripID] = true
			out.tripRedSites[r.TripID] = dedupe(append(out.tripRedSites[r.TripID], r.SiteID))
		}
	}
	for id := range out.trip {
		delete(out.tripAmberOnly, id)
	}
	return out, nil
}

// silencesPlain is the R3 rule (James, 2026-10-04) for a trip's own row, by
// the row's CURRENT status at this post: a red or kind-less ack silences
// whatever the row is; an amber-only ack silences the row only while it IS
// amber. ⟦was: "unless it is red" — so the ORIGIN's pre-due COUNTDOWN row,
// which never showed an amber, read "(ACKNOWLEDGED BY ICE PAD)" and offered
// "Un-acknowledge — put this alert back" on the far end's amber ack (R6-1.6a,
// fix4 H 2026-10-05). A countdown has nothing for an amber ack to silence;
// it now carries the VERIFY-only face and no un-ack, as the red row does.⟧
func (s ackSets) silencesPlain(tripID string, status Status) bool {
	if s.trip[tripID] {
		return true
	}
	return s.tripAmberOnly[tripID] && status == StatusOverdueUnresolved
}

// OverdueAckedBy (FIX14, James 2026-10-05: "make the primary server of the
// dispatch page NDB") is, per trip, the sites whose EFFECTIVE red or
// kind-less acknowledgment stands — the same set that silences the trip's
// row at both ends (silencesPlain), with who logged it. It is the one piece
// of an end's deadline verdict that replicates: a red ack is stamped by the
// acknowledging post from its OWN board (AlertAcknowledged.AlertKind), so it
// proves that post had the run OVERDUE. A post on neither end of a run (the
// dispatch view served by a third post) reads it to say "acknowledged as
// overdue at <post>" instead of guessing. Amber-only acks prove nothing
// about a red and are not in it.
func OverdueAckedBy(st *store.Store) (map[string][]string, error) {
	s, err := ackedTrips(st)
	if err != nil {
		return nil, err
	}
	return s.tripRedSites, nil
}

func tripKnown(st *store.Store, tripID string) (bool, error) {
	rows, err := dumpSection[tripRow](st, "trip", store.Eq{Column: "trip_id", Value: tripID})
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}
