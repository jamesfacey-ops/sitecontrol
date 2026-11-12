// w9c_seams_test.go — W9c (item 17, F6's alert-ack retraction:
// `alert_ack_retracted`): shared fixture machinery for the fold+Evaluate
// side of W9c. NO ASSERTIONS LIVE HERE.
//
// ⚠ THE ONE DESIGN DECISION IN THIS FILE, stated first because every other
// W9c test rests on it.
//
// W9c is greenfield in all of its own artefacts. On the pre-chunk tree
// `grep -ra 'AlertAckRetracted' --include=*.go app/` returns ZERO and
// `grep -ra '/alerts/unack\|Unack\|unAck\|UnAck' --include=*.go app/`
// returns ZERO; the only `alert_ack_retract` hits in the tree are comments
// and a planted control string inside
// `internal/domain/w4_registry_test.go` [MEASURED, this pass]. A test that
// NAMES `deadline.AlertAckRetracted` or `deadline.RetractAck` does not
// fail — it fails to COMPILE, one compile error in `package deadline`
// buries every other W9c red behind it, and it breaks every other agent's
// `go test ./internal/deadline` while W6/W7/W8/W9a/W9b are live. W5a met
// this fork and recorded the answer (`W5a-tests.md` §1.1); W6 met it again
// and chose runtime reds (`W6-tests.md` §1.1); W9a followed W6. W9c
// follows W9a.
//
// So NOTHING in W9c's tests names an unbuilt Go symbol. Instead:
//
//   - retractions (and, where a fixture must state its own log keys, acks)
//     are authored as HAND-BUILT ENVELOPES carrying the frozen wire JSON of
//     F6 §2, applied through the real `store.Apply` — which is deliberately
//     permissive about site_id ("injecting foreign-site events in tests is
//     exactly what makes C5/C6 buildable before replication exists",
//     store/log.go). That is the WIRE PATH F6 §7 names for T2b/T3/T9, and
//     it freezes gate B-2's payload literals by construction;
//   - the effect is observed at `Deadlines.Evaluate()` / `Board.Banner()` /
//     `Alert.Acked` — the consumers the criteria name — never at an
//     unbuilt predicate function;
//   - raw log content goes through a second `sql.Open("sqlite", st.Path)`
//     handle, the technique `w5d_sidetable_structural_test.go` established
//     ([[D-56]]: never through an HTTP dump). ⚠ `store.EachEvent` — the
//     seam W9c-6 and W9c-9 name for their event counts — DOES NOT EXIST on
//     the delivered tree: it is on W1's Amendment-2 allowlist as a future
//     reader (`internal/store/w1_eventtime_gone_test.go:280`) and is on no
//     `*Store` today [MEASURED]. `w9cLogCount` is the total-log count that
//     criterion means, read straight off `event_log`, which cannot miss a
//     stream the way a per-(site,epoch) walk can. Recorded as a criteria→
//     tree delta in `reports/window-build/W9c-tests.md`.
//
// Consequence, wanted: every W9c test COMPILES AND RUNS against the
// pre-chunk tree, so its pre-chunk failure is a produced RUNTIME red naming
// its criterion, never a compile error. The builder's Go names and
// signatures stay the builder's.
package deadline

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// --- the frozen wire spellings, as literals (F6 §2 and §3.1; gate B-2's
// list). A builder that spells one of them differently reds every fixture
// that carries it. ---

const (
	w9cEvtAckRetracted = "alert_ack_retracted"
	w9cEvtAck          = "alert_acknowledged"

	// alert_ack_retracted payload keys (F6 §2)
	w9cKeyType         = "type"
	w9cKeyAckSiteID    = "ack_site_id"
	w9cKeyAckNodeEpoch = "ack_node_epoch"
	w9cKeyAckSeq       = "ack_seq"
	w9cKeyRecordedBy   = "recorded_by"

	// alert_acknowledged payload keys (deadline/ack.go, the existing type)
	w9cKeyTripID       = "trip_id"
	w9cKeyReviewPhase  = "review_phase"
	w9cKeyAckedAtBoard = "acked_at_board"
)

// w9cPadTripID is the trip id the stream-padding events name. It is
// deliberately a trip that does not exist: a pad event must move a stream's
// seq and touch nothing a criterion asserts on. Padding uses
// `alert_acknowledged`, an event type that already exists and already
// folds, rather than a made-up type — so no new replicated event-type
// literal enters the tree from a fixture.
const w9cPadTripID = "w9c-pad-no-such-trip"

// w9cKey is one log key — the identity F6 §2 targets and F6 §3.3 calls
// "the FULL THREE-COLUMN key".
type w9cKey struct {
	Site  string
	Epoch int64
	Seq   int64
}

func (k w9cKey) String() string {
	return fmt.Sprintf("(%s, epoch %d, seq %d)", k.Site, k.Epoch, k.Seq)
}

// w9cEnvelope builds ONE replicated envelope by hand, in the WIRE shape.
// TimeSource is "default" and OccurredAt == AuthoredAt.WallMS, the rule-W
// equality — an envelope violating it would be a fixture defect, not a W9c
// finding.
func w9cEnvelope(k w9cKey, wallMS int64, payload map[string]any) store.Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("w9c: marshalling a fixture payload: %v", err))
	}
	return store.Envelope{
		SiteID:        k.Site,
		NodeEpoch:     k.Epoch,
		Seq:           k.Seq,
		SchemaVersion: store.SchemaVersion,
		OriginAuth:    k.Site,
		AuthoredAt:    store.AuthoredAt{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced},
		OccurredAt:    wallMS,
		TimeSource:    "default",
		Payload:       raw,
	}
}

// w9cNextSeq is the seq the next event on (site, epoch) will take.
func w9cNextSeq(t *testing.T, st *store.Store, site string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(site, epoch)
	if err != nil {
		t.Fatalf("w9c: watermark(%s, %d): %v", site, epoch, err)
	}
	return wm + 1
}

// w9cPadTo advances (site, epoch) so that the NEXT event on it lands at
// exactly `want`. It fails loudly when the stream is already past `want` —
// a fixture that silently gave up on its stated seq would be a fixture
// whose "stated literally" was a comment.
func w9cPadTo(t *testing.T, st *store.Store, site string, epoch, want int64) {
	t.Helper()
	next := w9cNextSeq(t, st, site, epoch)
	if next > want {
		t.Fatalf("w9c fixture: %s epoch %d is already at seq %d — cannot place the next event at the stated seq %d", site, epoch, next-1, want)
	}
	for ; next < want; next++ {
		env := w9cEnvelope(w9cKey{site, epoch, next}, 1_700_000_000_000+next,
			map[string]any{w9cKeyType: w9cEvtAck, w9cKeyTripID: w9cPadTripID})
		w9cApply(t, st, env)
	}
}

// w9cApply applies one hand-built envelope and insists it was applied (not
// a duplicate, not a gap) — the fixture's own guard, so a mis-numbered
// stream shows up here rather than as a mysterious assertion failure later.
func w9cApply(t *testing.T, st *store.Store, env store.Envelope) store.Envelope {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9c: apply %v: %v", w9cKey{env.SiteID, env.NodeEpoch, env.Seq}, err)
	}
	if !applied {
		t.Fatalf("w9c: apply %v reported a duplicate — the fixture's stream numbering is wrong", w9cKey{env.SiteID, env.NodeEpoch, env.Seq})
	}
	return env
}

// w9cRetractionEnvelope builds the F6 §2 retraction naming `target`,
// authored at `at`. `at.Seq == 0` means "the next free seq on that stream".
func w9cRetractionEnvelope(t *testing.T, st *store.Store, at, target w9cKey, recordedBy string, wallMS int64) store.Envelope {
	t.Helper()
	if at.Seq == 0 {
		at.Seq = w9cNextSeq(t, st, at.Site, at.Epoch)
	}
	p := map[string]any{
		w9cKeyType:         w9cEvtAckRetracted,
		w9cKeyAckSiteID:    target.Site,
		w9cKeyAckNodeEpoch: target.Epoch,
		w9cKeyAckSeq:       target.Seq,
	}
	if recordedBy != "" {
		p[w9cKeyRecordedBy] = recordedBy
	}
	return w9cEnvelope(at, wallMS, p)
}

// w9cRetract authors + applies one retraction naming `target`. Returns the
// retraction's own key.
func w9cRetract(t *testing.T, st *store.Store, at, target w9cKey, recordedBy string, wallMS int64) w9cKey {
	t.Helper()
	env := w9cApply(t, st, w9cRetractionEnvelope(t, st, at, target, recordedBy, wallMS))
	return w9cKey{env.SiteID, env.NodeEpoch, env.Seq}
}

// w9cAckEnvelope builds an `alert_acknowledged` in the wire shape, so a
// fixture can state the ack's OWN log key literally (which no call through
// Acknowledge can do — it takes whatever seq the stream is at).
func w9cAckEnvelope(at w9cKey, tripID string, reviewPhase *int64, ackedAtBoard *string, recordedBy string, wallMS int64) store.Envelope {
	p := map[string]any{w9cKeyType: w9cEvtAck, w9cKeyTripID: tripID}
	if reviewPhase != nil {
		p[w9cKeyReviewPhase] = *reviewPhase
	}
	if ackedAtBoard != nil {
		p[w9cKeyAckedAtBoard] = *ackedAtBoard
	}
	if recordedBy != "" {
		p[w9cKeyRecordedBy] = recordedBy
	}
	return w9cEnvelope(at, wallMS, p)
}

// w9cAckAt places a plain ack at the STATED key, padding the stream to it
// first.
func w9cAckAt(t *testing.T, st *store.Store, at w9cKey, tripID, recordedBy string, wallMS int64) w9cKey {
	t.Helper()
	w9cPadTo(t, st, at.Site, at.Epoch, at.Seq)
	w9cApply(t, st, w9cAckEnvelope(at, tripID, nil, nil, recordedBy, wallMS))
	return at
}

// w9cAckHere authors a REAL plain ack through deadline.Acknowledge on
// (here, 1) — the production authoring path — and returns the log key it
// took, recovered from the watermark. Used wherever a criterion needs a
// genuine ack (attribution stamps, tripKnown refusals) rather than a stated
// key.
func w9cAckHere(t *testing.T, st *store.Store, tripID string, recordedBy *string) w9cKey {
	t.Helper()
	want := w9cNextSeq(t, st, here, 1)
	if err := Acknowledge(st, here, 1, tripID, recordedBy, 0, "default"); err != nil {
		t.Fatalf("w9c: Acknowledge(%s): %v", tripID, err)
	}
	got := w9cNextSeq(t, st, here, 1)
	if got != want+1 {
		t.Fatalf("w9c: Acknowledge authored %d events, want exactly 1", got-want)
	}
	return w9cKey{here, 1, want}
}

// --- raw log reads (never an HTTP dump — D-56) ---

// w9cLogCount is every event in the log, over every stream. This is the
// quantity W9c-6 and W9c-9 mean by "`EachEvent` = pre-POST + N EXACTLY";
// counting off `event_log` rather than walking (site, epoch) pairs means a
// handler that authored on an unexpected stream is still counted, which a
// per-stream walk would silently miss.
func w9cLogCount(t *testing.T, st *store.Store) int {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM event_log`).Scan(&n); err != nil {
		t.Fatalf("w9c: counting event_log: %v", err)
	}
	return n
}

// w9cPayloadsOfType returns every event_log payload whose "type" is typ, in
// (site_id, node_epoch, seq) order, each decoded — the "nothing deleted"
// and "one retraction per target" evidence.
func w9cPayloadsOfType(t *testing.T, st *store.Store, typ string) []map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT payload FROM event_log ORDER BY site_id, node_epoch, seq`)
	if err != nil {
		t.Fatalf("w9c: reading event_log: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			continue // not an object payload; nothing of ours
		}
		if s, _ := p[w9cKeyType].(string); s == typ {
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// w9cHasEvent reports whether the log still holds the event at k — the
// "both events still in the log, nothing deleted" half of W9c-1/W9c-2.
func w9cHasEvent(t *testing.T, st *store.Store, k w9cKey) bool {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM event_log WHERE site_id = ? AND node_epoch = ? AND seq = ?`,
		k.Site, k.Epoch, k.Seq).Scan(&n); err != nil {
		t.Fatalf("w9c: looking up %v: %v", k, err)
	}
	return n == 1
}

// w9cRetractionTargets is every retraction in the log, as the target key it
// names. Order is (site_id, node_epoch, seq) of the RETRACTIONS.
func w9cRetractionTargets(t *testing.T, st *store.Store) []w9cKey {
	t.Helper()
	var out []w9cKey
	for _, p := range w9cPayloadsOfType(t, st, w9cEvtAckRetracted) {
		site, _ := p[w9cKeyAckSiteID].(string)
		ep, _ := p[w9cKeyAckNodeEpoch].(float64)
		sq, _ := p[w9cKeyAckSeq].(float64)
		out = append(out, w9cKey{site, int64(ep), int64(sq)})
	}
	return out
}

// --- board helpers ---

// w9cBannerRows is every Banner() row for one trip.
func w9cBannerRows(b Board, tripID string) []Alert {
	var out []Alert
	for _, a := range b.Banner() {
		if a.TripID == tripID {
			out = append(out, a)
		}
	}
	return out
}

// w9cBannerHas reports whether the trip is ALERTING — i.e. it reaches the
// banner at all.
func w9cBannerHas(b Board, tripID string) bool { return len(w9cBannerRows(b, tripID)) > 0 }

// w9cBannerStatuses renders a banner as a diffable string, for failure
// messages that say what the board actually showed.
func w9cBannerStatuses(b Board) string {
	out := ""
	for _, a := range b.Banner() {
		out += fmt.Sprintf("[%s %s acked=%v] ", a.TripID, a.Status, a.Acked)
	}
	if out == "" {
		return "(empty banner)"
	}
	return out
}
