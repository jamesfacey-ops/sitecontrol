// w1_version_gate_test.go — W1-2: "The v10 binary refuses a v9 database at
// open and refuses a v9 envelope at ApplyBatch — re-proven at v10, not
// assumed from the v9 tests." (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27.)
//
// ⟦SchemaVersion 11 — RE-PINNED 2026-09-05⟧ The criterion is about THE
// deployment edge, and the edge moved: the binary is v11 and the literal
// predecessor is 10 — the version the demo pair actually ran, so a stale
// database or a not-yet-upgraded peer is a schema-10 one, not a schema-9
// one. Both refusals ([[D-39]]: store.go's Open gate and log.go's
// ApplyBatch gate) are re-proven at 11 here, on the literal 10, never on
// SchemaVersion-1. ⟦was: v10 / literal 9 throughout⟧
//
// KILLER MUTANT: m6 — a version gate that admits a predecessor database at
// open, or a predecessor envelope at ApplyBatch.
//
// EXPECTED RED (pre-build): store.SchemaVersion is still 9 at HEAD 89ecee6,
// so ⟦was: "TestW1SchemaVersionIsTen"⟧ fails on the constant, a stored marker of '9'
// opens cleanly, and an envelope stamped 9 applies. All three assertions
// below fail against the unfixed tree — that pre-build red is this file's
// ledger RED row. (Historical; the 10→11 re-pin keeps the shape and moves
// the literals.)
package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// W1: SchemaVersion 9→10 landed in that chunk, first, and everything after
// built against v10 (§3 W1 "Changes"). The D-92/D-88/D-91/D-98 increment
// bumps 10→11, once, at the head of its train. Pinned as an equality so a
// partial rebase cannot ride an unbumped constant to a green suite.
// ⟦was: TestW1SchemaVersionIsTen, `SchemaVersion != 10`⟧
// ⟦was: TestW1SchemaVersionIsEleven, stamping 11⟧ SchemaVersion 12
// (R-16, 2026-09-11): entry-on-completion + the import grandfathering
// marker.
// SchemaVersion 13 (R-17): re-pinned from 12, following this file's own
// established pattern (its neighbour below was itself renamed
// ⟦was: "TestW1V11BinaryRefusesAV10DatabaseAtOpen"⟧ at the 11→12 bump) —
// each bump restamps this pair to the new literal predecessor edge rather
// than leaving a stale one proven forever.
// SchemaVersion 14 (R-18, 2026-09-13): re-pinned again, same discipline —
// ⟦was: "TestW1SchemaVersionIsThirteen", `SchemaVersion != 13`, "13 (the
// 12→13 increment; store.go's bump log)"⟧.
// SchemaVersion 15 (R-20 + R-23, 2026-09-13, the rejoin path: visit carries
// its minting key, is_open is derived): re-pinned again — ⟦was:
// "TestW1SchemaVersionIsFourteen", `SchemaVersion != 14`, "14 (the 13→14
// increment; store.go's bump log)"⟧.
// SchemaVersion 16 (R3 alert_kind, 2026-10-04, fix round 2 — the sole bump
// of that round): re-pinned again — ⟦was: "TestW1SchemaVersionIsFifteen",
// `SchemaVersion != 15`, "15 (the 14→15 increment; store.go's bump log)"⟧.
func TestW1SchemaVersionIsSixteen(t *testing.T) {
	if SchemaVersion != 16 {
		t.Fatalf("store.SchemaVersion = %d, want 16 (the 15→16 increment; store.go's bump log)", SchemaVersion)
	}
}

// W1-2 first half: the v16 binary must refuse a database stored under
// schema_version 15 at Open — the LITERAL predecessor version, not a
// synthetic 999 (the v9 suite's TestOpenRefusesForeignSchemaVersion proves
// only the generic foreign-marker refusal; this re-proves the 15-vs-16 edge
// a real outage would cross). Asserted on the substring
// "schema_version 15 " with its trailing space, so a `15` inside the store
// PATH cannot satisfy it (W6/W7/W8's recorded instrument weakness).
// ⟦was: TestW1V13BinaryRefusesAV12DatabaseAtOpen, stamping '12'/'13'⟧
// ⟦was: TestW1V14BinaryRefusesAV13DatabaseAtOpen, stamping '13'/'14'⟧
// ⟦was: TestW1V15BinaryRefusesAV14DatabaseAtOpen, stamping '14'/'15'⟧
func TestW1V16BinaryRefusesAV15DatabaseAtOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "predecessor.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Simulate the pre-increment database: marker written by the v15 binary.
	if _, err := s.db.Exec(`UPDATE meta SET value = '15' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("set stored version to 15: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// m6 (open half): a gate that admits the v15 marker makes this succeed.
	if _, err := Open(dbPath); err == nil {
		t.Fatal("Open proceeded over a schema_version 15 database; the v16 binary must refuse (W1-2, mutant m6)")
	} else if !strings.Contains(err.Error(), "schema_version 15 ") || !strings.Contains(err.Error(), "requires 16") || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("refusal must name the stored version, the required version and refuse explicitly, got: %v", err)
	}
}

// W1-2 second half: ApplyBatch refuses an envelope stamped schema_version 10
// with the typed *SchemaVersionError, writing nothing — the D21-REPLAY §5.4
// refusal re-established on the new binary rather than assumed from the v9
// suite (whose fixtures used SchemaVersion+1, a version that has never been
// the real predecessor). This is the peer-envelope half of [[D-39]]'s
// refusal (log.go's ApplyBatch gate), confirmed to fire on 11.
// ⟦was: TestW1ApplyBatchRefusesAV9EnvelopeWithTypedErrorAndWritesNothing,
// stamping 9⟧
func TestW1ApplyBatchRefusesAV10EnvelopeWithTypedErrorAndWritesNothing(t *testing.T) {
	s := newTestStore(t)
	env := Envelope{
		SiteID:        "site-x",
		NodeEpoch:     1,
		Seq:           1,
		SchemaVersion: 10, // the literal predecessor, not store.SchemaVersion-1
		OriginAuth:    "site-x",
		Payload:       []byte(`{"type":"tick","k":"a"}`),
	}
	results, err := s.ApplyBatch([]Envelope{env})
	if len(results) != 0 {
		t.Fatalf("a v10 envelope was applied (results=%v); ApplyBatch must refuse before writing (W1-2, mutant m6)", results)
	}
	var sve *SchemaVersionError
	if !errors.As(err, &sve) {
		t.Fatalf("want *SchemaVersionError for a v10 envelope, got %v", err)
	}
	if sve.Got != 10 || sve.Want != SchemaVersion {
		t.Fatalf("SchemaVersionError = got %d want-field %d; expected got=10 want=%d", sve.Got, sve.Want, SchemaVersion)
	}
	// Nothing written: watermark still zero, log empty.
	wm, werr := s.Watermark("site-x", 1)
	if werr != nil {
		t.Fatal(werr)
	}
	if wm != 0 {
		t.Fatalf("watermark advanced to %d over a refused v10 envelope; must stay 0", wm)
	}
	evs, eerr := s.Events("site-x", 1, 1, 0)
	if eerr != nil {
		t.Fatal(eerr)
	}
	if len(evs) != 0 {
		t.Fatalf("event_log holds %d rows after a refused v10 envelope; must hold none", len(evs))
	}
}
