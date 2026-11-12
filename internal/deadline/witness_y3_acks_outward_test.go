// witness_y3_acks_outward_test.go — the witness phase's "work outward" step
// (reports/witness/_BRIEF.md, y3-acks assignment) for the remaining
// unreached blocks in internal/deadline/unack.go that are NOT among the
// U-row-Y census's 34 named survivors (row-y-evidence/blocks19_unc.tsv,
// filtered to this file). These plants are AUTHORED HERE, not in
// row-y-evidence/plants.py — see reports/witness/y3-acks.md for the
// line-anchored definitions and the run-both-ways record.
package deadline

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	msqlite "modernc.org/sqlite"
	"sitecontrol/internal/store"
)

// witnessRawEnvelope builds one hand-made envelope carrying an EXACT raw
// payload — unlike w9cEnvelope (w9c_seams_test.go), which always marshals a
// map and so can never produce a payload json.Valid rejects or a
// type-mismatched field, both of which D4 needs to reach the fold's own
// json.Unmarshal rather than being turned away at envelope validation.
func witnessRawEnvelope(k w9cKey, wallMS int64, payload []byte) store.Envelope {
	return store.Envelope{
		SiteID:        k.Site,
		NodeEpoch:     k.Epoch,
		Seq:           k.Seq,
		SchemaVersion: store.SchemaVersion,
		OriginAuth:    k.Site,
		AuthoredAt:    store.AuthoredAt{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced},
		OccurredAt:    wallMS,
		TimeSource:    "default",
		Payload:       payload,
	}
}

// TestWitnessD1EffectiveAcksPropagatesTheAckDumpError — unack.go:230-232,
// EffectiveAcks' FIRST error arm (`dumpSection[ackRow]` failing).
//
// ⚠ MEASUREMENT NOTE: closing the whole store does NOT discriminate this
// arm from its neighbour — it was tried first, and it was wrong. With the
// whole store closed, disabling ONLY this arm's check still leaves the
// function erroring, because execution falls through to
// retractionsByTarget's own dumpSection call, which fails on the very same
// closed store and is still caught by the SECOND arm (line 234) or by
// retractionsByTarget's own (line 180). A test built that way would pass
// clean AND pass planted — proving nothing. Dropping ONLY the `alert_ack`
// table isolates the fault to this exact call: `alert_ack_retraction`
// stays queryable, so retractionsByTarget succeeds and the SECOND arm
// never fires — only THIS arm stands between the fault and a clean-looking
// empty result.
//
// PLANT: `if false && err != nil { return nil, err }` — the dump failure
// is swallowed and the function falls through with a nil `acks` slice,
// retractionsByTarget succeeds (untouched table), and EffectiveAcks
// returns ([], nil): a DATABASE ERROR reads as "this trip has zero acks",
// an empty, ERROR-FREE result — the false-zero class.
func TestWitnessD1EffectiveAcksPropagatesTheAckDumpError(t *testing.T) {
	st := newSite(t)
	witnessDropTable(t, st.Path, "alert_ack")

	_, err := EffectiveAcks(st, "any-trip-id")
	if err == nil {
		t.Fatalf("EffectiveAcks with alert_ack dropped = <nil error>, want the dump failure surfaced, not a silent empty result")
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: a bare
	// `err != nil` check here is arm-blind — it also passes when
	// `alert_ack_retraction` is the table dropped instead (a DIFFERENT
	// guard, unack.go:234/180, firing on the SAME call path). Pin the
	// assertion to the fault THIS fixture guarantees so a future rung that
	// reads `alert_ack_retraction` before this arm can't turn this witness
	// into a false green.
	// ⚠ "alert_ack" is a PREFIX of "alert_ack_retraction" — a plain
	// strings.Contains(err, "no such table: alert_ack") would ALSO match
	// the sibling's failure text, so pin to store's own quoted section
	// name instead, which cannot collide (`dump projection "alert_ack"` vs
	// `dump projection "alert_ack_retraction"`, the closing quote lands
	// right after the exact table name).
	const wantCause = `dump projection "alert_ack"`
	if !strings.Contains(err.Error(), wantCause) {
		t.Fatalf("EffectiveAcks error = %q, want it to name the dropped table via %s — otherwise this witness "+
			"cannot tell this arm apart from unack.go:234/180's own dump failure", err.Error(), wantCause)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "D1", err.Error())
	}
}

// TestWitnessD2RetractAcksPropagatesTheAuthorError — unack.go:300-302,
// RetractAcks' Author error arm. Also the only path that exercises
// AckKey.String() (unack.go:86-88, FUNC-NEVER-ENTERED on the census):
// String() is what renders `k` into the wrapped error text below.
//
// PLANT: `if _, err := st.Author(...); false && err != nil` — an authoring
// failure is swallowed. RetractAcks then reports SUCCESS (nil error) to its
// caller despite authoring NOTHING: the worst version of this bug class,
// because the officer's un-ack click would read as having worked.
func TestWitnessD2RetractAcksPropagatesTheAuthorError(t *testing.T) {
	st := newSite(t)
	trip, _ := dispatchInbound(t, st, 60, "driver-1")
	if err := Acknowledge(st, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: Acknowledge: %v", err)
	}
	targets, err := EffectiveAcks(st, trip.TripID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("fixture: EffectiveAcks = %v, %v, want exactly one target", targets, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture: closing the store: %v", err)
	}
	err = RetractAcks(st, here, 1, targets, nil, 0, "default")
	if err == nil {
		t.Fatalf("RetractAcks on a closed store = <nil error>, want the Author failure surfaced naming target %v — a swallowed error here means a click that authors NOTHING is reported as success", targets[0])
	}
	if !strings.Contains(err.Error(), targets[0].String()) {
		t.Fatalf("RetractAcks error %q does not name the failing target via AckKey.String() (%v)", err.Error(), targets[0])
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses).
	// database/sql exports no sentinel for a closed DB, so the chain is checked
	// at its ROOT instead: walked through every Unwrap, the innermost error
	// must be the raw database/sql fault, never one of our own prefixed wrappers.
	witnessRequireRawRoot(t, "D2", err)
}

// witnessDropTable drops a projection table through a SECOND sqlite handle
// on the same file (the sanctioned raw-log technique, w9c_seams_test.go),
// so `st`'s own handle starts failing on that table while every other table
// stays intact — the fault injection D3/D6 need to reach a SPECIFIC
// dumpSection/tx.Exec call without touching production code.
func witnessDropTable(t *testing.T, path, table string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("witness fixture: opening a second handle on %s: %v", path, err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("witness fixture: dropping %s: %v", table, err)
	}
}

// TestWitnessD3EffectiveAcksPropagatesTheRetractionDumpError — two blocks
// at once: unack.go:234-236 (EffectiveAcks' SECOND error arm, wrapping
// retractionsByTarget) and unack.go:180-182 (retractionsByTarget's own
// error arm) — both unreachable by simply closing the whole store (D1
// already fails on the FIRST dump, before either of these runs), so this
// drops only `alert_ack_retraction`, leaving `alert_ack` queryable.
//
// PLANT (either site): swallow the error the same way as D1. The same
// false-zero consequence: a broken retraction-table read reads as "nothing
// is retracted", which is the SAFE direction for silencing (more alerting,
// never less) but still a database failure reported as a clean answer.
func TestWitnessD3EffectiveAcksPropagatesTheRetractionDumpError(t *testing.T) {
	st := newSite(t)
	trip, _ := dispatchInbound(t, st, 60, "driver-1")
	if err := Acknowledge(st, here, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: Acknowledge: %v", err)
	}
	witnessDropTable(t, st.Path, "alert_ack_retraction")

	_, err := EffectiveAcks(st, trip.TripID)
	if err == nil {
		t.Fatalf("EffectiveAcks with alert_ack_retraction dropped = <nil error>, want the retraction-dump failure surfaced (the alert_ack read must still succeed, isolating the failure to the retraction half)")
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: a bare
	// `err != nil` check is arm-blind between THIS test's two own arms
	// (unack.go:234 and :180, both dumping alert_ack_retraction — declared
	// as a linear pair, not two independent kills) and D1's arm (unack.go
	// :230, dumping alert_ack). Pin the assertion to the table this fixture
	// guarantees is gone.
	const wantCause = "no such table: alert_ack_retraction"
	if !strings.Contains(err.Error(), wantCause) {
		t.Fatalf("EffectiveAcks error = %q, want it to name the dropped table (%q) — otherwise this witness "+
			"cannot tell this failure apart from D1's own dump failure on a working retraction table", err.Error(), wantCause)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "D3", err.Error())
	}
}

// TestWitnessD4RetractedFoldRejectsATypeMismatchedPayload — unack.go:139-141,
// applyAlertAckRetracted's json.Unmarshal error arm.
//
// store.Envelope.Validate rejects a syntactically-INVALID JSON payload
// before the fold ever runs (envelope.go:116, `json.Valid`), so that
// half of this arm is structurally unreachable through Apply. A payload
// that is valid JSON but TYPE-MISMATCHED against AlertAckRetracted's fields
// (a string where ack_node_epoch wants an int64) passes envelope validation
// and still fails json.Unmarshal — which is what this fixture sends.
//
// PLANT: `if false && err != nil { return fmt.Errorf(...) }` — the
// unmarshal error is swallowed and the fold proceeds with a PARTIALLY-
// POPULATED, zero-valued struct, inserting a phantom retraction row (a
// garbage target key, e.g. epoch 0) into the append-only log — a malformed
// event that should have been refused is instead accepted permanently
// (D-1: nothing is ever deleted).
func TestWitnessD4RetractedFoldRejectsATypeMismatchedPayload(t *testing.T) {
	st := newSite(t)
	env := witnessRawEnvelope(w9cKey{here, 1, 1}, 1_700_000_000_000,
		[]byte(`{"type":"alert_ack_retracted","ack_site_id":"x","ack_node_epoch":"not-a-number","ack_seq":1}`))
	_, err := st.Apply(env)
	if err == nil {
		t.Fatalf("Apply(type-mismatched alert_ack_retracted payload) = <nil error>, want the fold's unmarshal failure to reject the whole envelope")
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: this shares
	// D6's wrap prefix ("deadline: ack retraction fold:") — a bare
	// `err != nil` check cannot tell THIS unmarshal failure apart from D6's
	// INSERT failure. Pin the assertion to the exact type mismatch this
	// fixture guarantees: a *json.UnmarshalTypeError naming ack_node_epoch,
	// not any other refusal that happens to share the prefix.
	var terr *json.UnmarshalTypeError
	if !errors.As(err, &terr) {
		t.Fatalf("Apply error = %q, want it to wrap a *json.UnmarshalTypeError — otherwise this witness "+
			"cannot tell this unmarshal failure apart from D6's own INSERT failure sharing the same wrap prefix", err.Error())
	}
	if terr.Field != "ack_node_epoch" {
		t.Fatalf("Apply error's *json.UnmarshalTypeError names field %q, want %q — the field this fixture's payload actually corrupts", terr.Field, "ack_node_epoch")
	}
}

// TestWitnessD6RetractedFoldPropagatesTheInsertError — unack.go:149-151,
// applyAlertAckRetracted's tx.Exec error arm.
//
// PLANT: `); false && err != nil { return fmt.Errorf(...) }` — an insert
// failure is swallowed, so the fold reports the event as successfully
// applied (raw log row committed, watermark advanced) despite the
// projection never actually gaining the row it exists to hold: derived
// state is no longer f(raw), the exact defect class store/log.go's
// applyBatch comment names as the reason a fold error unwinds the WHOLE
// batch rather than committing a partial write.
func TestWitnessD6RetractedFoldPropagatesTheInsertError(t *testing.T) {
	st := newSite(t)
	witnessDropTable(t, st.Path, "alert_ack_retraction")

	env := witnessRawEnvelope(w9cKey{here, 1, 1}, 1_700_000_000_000,
		[]byte(`{"type":"alert_ack_retracted","ack_site_id":"site-a","ack_node_epoch":1,"ack_seq":1}`))
	_, err := st.Apply(env)
	if err == nil {
		t.Fatalf("Apply(retraction) with alert_ack_retraction dropped = <nil error>, want the INSERT failure surfaced rather than a silently-accepted event with no projection row behind it")
	}
	// [[reports/witness/u1b-domain.md finding 🟠2]] MEASURED: this shares
	// D4's wrap prefix ("deadline: ack retraction fold:") — a bare
	// `err != nil` check cannot tell THIS insert failure apart from D4's
	// unmarshal failure. Pin the assertion to the table this fixture
	// guarantees is gone.
	const wantCause = "no such table: alert_ack_retraction"
	if !strings.Contains(err.Error(), wantCause) {
		t.Fatalf("Apply error = %q, want it to name the dropped table (%q) — otherwise this witness cannot "+
			"tell this INSERT failure apart from D4's own unmarshal failure sharing the same wrap prefix", err.Error(), wantCause)
	}

	// [[PCTW-SWEEP]] The Contains checks above are blind to a lost %w: a %w→%s
	// swap leaves .Error() byte-identical while breaking the chain (MEASURED:
	// this test stayed green under that swap at every site it crosses). Only
	// the TYPE can see it — the driver's own *sqlite.Error must still be
	// reachable through every layer by errors.As.
	var sqlErr *msqlite.Error
	if !errors.As(err, &sqlErr) {
		t.Errorf("%s: error %q does not reach the driver's *sqlite.Error via errors.As — a %%w between this arm and the dropped-table fault has been lost (the text is unchanged, so no Contains check can see this)", "D6", err.Error())
	}
}

// TestWitnessD7JoinNamesEmptyCase — unack.go:387-388, joinNames' `case 0`.
//
// Both real callers (ackFace, repudiationFace) already return "" before
// ever calling joinNames on an empty list, so this case is DEAD CODE on
// every currently reachable path — recorded here, not repaired (tests
// only). A direct call is the only way to witness it at all.
//
// PLANT: delete `case 0: return ""` — joinNames(nil) would then fall into
// `case 1: return in[0]`'s FALSE match and on into the general branch,
// where `head := in[:len(in)-1]` on a zero-length slice underflows to
// `in[:-1]` and panics.
func TestWitnessD7JoinNamesEmptyCase(t *testing.T) {
	if got := joinNames(nil); got != "" {
		t.Fatalf("joinNames(nil) = %q, want \"\"", got)
	}
	if got := joinNames([]string{}); got != "" {
		t.Fatalf("joinNames([]string{}) = %q, want \"\"", got)
	}
}

// witnessRequireRawRoot walks err through every Unwrap and fails if the
// innermost error still carries one of this module's own prefixes. A %w→%s
// swap at ANY layer leaves .Error() byte-identical but makes that layer the
// root, so this is the one assertion strings.Contains cannot fake.
// [[PCTW-SWEEP]] (2026-09-06).
func witnessRequireRawRoot(t *testing.T, tag string, err error) {
	t.Helper()
	root := err
	for {
		u := errors.Unwrap(root)
		if u == nil {
			break
		}
		root = u
	}
	for _, p := range []string{"domain: ", "store: ", "deadline: "} {
		if strings.HasPrefix(root.Error(), p) {
			t.Errorf("%s: the innermost error is %q — one of our own wrappers, so a %%w between it and the underlying fault has been lost (the full text is unchanged, so no Contains check can see this)", tag, root.Error())
			return
		}
	}
}
