// w5e_schema_version_test.go — WITHDRAWN.
//
// ── WITHDRAWN by reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01) ───────
//
// This file held exactly one test, ⟦was: "TestW5eSchemaVersionBumpedForTheTripShapeChange"⟧,
// and its fixture helper `w5eStampSchemaVersion` (189 lines total). Its whole
// subject was W5-X5's requirement that W5e bump `store.SchemaVersion` 10 → 11
// for `trip.cargo_note`. **That requirement is STRUCK.**
//
// The schema-10 window carries exactly ONE version increment — W1's 9 → 10 at
// commit 49abf65 — and every W5 sub-chunk rides it. The v10 log starts empty
// (D-29); design/WINDOW-BUILD-PLAN-v1.md §7 step 2 DELETES every database at
// the cutover; one hash-verified binary ships to both posts. So no stored
// database and no peer ever meets a differently-shaped binary inside the
// window, and the plan's §8 bump-gate table prices `cargo_note` by name as
// "window-priced" (row 10). W3 and W4 each landed a new REPLICATED event type
// inside this window with no bump — the exact change store.go bumped 3→4 and
// 4→5 for — and no reviewer called it a defect, because §8 plans for them to
// ship inside the one bump.
//
// ⚠️ THE PROVENANCE IS KEPT BECAUSE IT IS NOT A CRITICISM OF THIS FILE.
// This file was written by W5e's BUILDER, correctly, against post-lock
// amendment §1 (`reports/W5-POSTLOCK-AMENDMENTS.md`), which had lifted W5e's
// W5-X5 exemption. §1's MEASUREMENTS were right and are NOT retracted — the two
// grounds it struck ("cargo_note's table is new"; "trip.go:371's index
// precedent covers it") were genuinely false. What was wrong was the REMEDY,
// and that error was already present in W5-X5 AS LOCKED, which required
// W5a/W5b/W5c to bump; §1 propagated it rather than introducing it. The
// builder implemented the specification it was given.
//
// ⚠️ THIS FILE WAS NEVER COMMITTED (untracked at HEAD 44e497e), so the
// withdrawn 189 lines are NOT recoverable from git history. Its produced red is
// transcribed in `reports/window-build/W5e-build.md` §3, and its three-part
// shape is described in `reports/window-build/W5e-tests.md` §7.1/§7.1a.
//
// WHAT STILL PROTECTS `cargo_note`: `W5-X4`, which introspects the landed
// column set — unaffected by this ruling — plus the two W1 tests, which already
// assert the form the ruling keeps, at the real deployment edge:
// ⟦was: "`TestW1SchemaVersionIsTen`", then "TestW1SchemaVersionIsEleven"⟧
// (w1_version_gate_test.go, now TestW1SchemaVersionIsSixteen) pins the exit
// version, and ⟦was: "`TestW1V10BinaryRefusesAV9DatabaseAtOpen`", then
// "TestW1V11BinaryRefusesAV10DatabaseAtOpen"⟧ (now
// TestW1V16BinaryRefusesAV15DatabaseAtOpen) asserts the untyped Open refusal
// over a store stamped the literal predecessor.
//
// Withdrawn by the plan-owner pass, not by a builder — the same route by which
// muster_test.go's live half was amended (post-lock amendments §5/§6).
// ─────────────────────────────────────────────────────────────────────────────
//
// ── AMENDED 2026-09-01 — plan-owner pass, recorded at §20.4 of
// reports/W5-POSTLOCK-AMENDMENTS.md, on W5-ADVERSARIAL-REVIEW.md A-4 ──────────
//
// THE WITHDRAWAL ABOVE STANDS. The struck within-window bump requirement is not
// resurrected here and no arm below asserts a bump. What is amended is the ONE
// line in which this tombstone discharged W5e's `W5-X5` row by pointing at
// ANOTHER CHUNK's green:
//
// ⟦was: "WHAT STILL PROTECTS `cargo_note`: `W5-X4` … plus the two W1 tests,
// which already assert the form the ruling keeps, at the real deployment edge:
// `TestW1SchemaVersionIsTen` … and `TestW1V10BinaryRefusesAV9DatabaseAtOpen`"⟧
//
// Post-lock amendment §15.3 made a ruling standing, W5-wide: each sub-chunk
// that owes `W5-X5` closes its row **EITHER** by carrying its own `'9'`-edge
// instance **OR** by an explicit ruling at its own gate that W1's two tests
// discharge it for that sub-chunk — "recorded as a ruling, never left blank."
// W5a's G-1 ruled the W1-borrow insufficient (`W5-X3`: no sub-chunk's green
// counts toward another's, and the same reasoning forbids reading another
// CHUNK's), and W5b's and W5c's gates each independently answered the same way.
// W5e's was the one row left blank — §16.1 says so in terms ("W5e is NOT closed
// by this pass") and A-4 measured it still blank.
//
// ⚠️ WHAT IS AND IS NOT STRUCK. The two W1 tests are landed, green, and NOT
// retracted; `W5-X4`'s protection of the column set is untouched. What is
// struck is READING EITHER OF THEM AS W5e's OWN `W5-X5` ROW.
//
// W5e takes the FIRST option, on W5a's/W5b's/W5c's pattern:
// `TestW5eColumnRidesTheV12ShapeAndAV11StoreIsRefused` below. Its fixture helper
// is written FRESH — the withdrawn `w5eStampSchemaVersion` is not recoverable
// (this file was never committed), which is the asymmetry A-4 flags: W5a's,
// W5b's and W5c's helpers were retained caller-less for exactly this re-point
// and W5e's was deleted with the tombstone.
// ─────────────────────────────────────────────────────────────────────────────
package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// TestW5eColumnRidesTheV12ShapeAndAV11StoreIsRefused is W5e's OWN `W5-X5` row,
// pointed at the '9' edge (post-lock amendment §15.3's first option; landed by
// a plan-owner pass on §16.2's precedent — a gate that writes the test it then
// blesses is not a gate). `W5-X5`'s three-part form, with W5e's column as the
// subject:
//
//	(iii) a store that carries `trip.cargo_note` reads back
//	      StoredSchemaVersion() == fmt.Sprint(store.SchemaVersion) AND == "12"
//	      ⟦re-pinned 2026-09-11 at the 11 -> 12 increment; was "11"⟧
//	      — the window's exit version, pinned. THIS IS THE KILLING ARM.
//	(i)   a store stamped the literal predecessor '11' is refused at Open by a
//	      non-nil UNTYPED error naming both versions and the word "refusing".
//	(ii)  the fixture stamps the LITERAL '11', never store.SchemaVersion-1.
//
// KILLER MUTANT: (m5-X5) as RE-POINTED by post-lock amendment §9 — "a
// sub-chunk that BUMPS SchemaVersion". Under it store.SchemaVersion is 12, the
// store carrying `cargo_note` stamps "12", and arm (iii) reds. Arms (i)/(ii)
// stay green under that mutant (a '9' store is refused by a 12 binary too),
// which is exactly why (iii) is pinned to a LITERAL and not only to a relation
// — an unpinned relation-only read-back is vacuous, as `W5-X5` says in terms.
//
// ⚠️ NOT A COPY OF W1's TESTS, and that is the whole point of the row.
// TestW1SchemaVersionIsSixteen reads the CONSTANT. This reads the version STAMPED
// IN THE STORE THAT CARRIES `trip.cargo_note`, so the column shape and the
// version are asserted of the SAME ARTEFACT. That is the assertion W5e owns and
// W1's constant-read does not make.
//
// ⚠️ W5e's column is on the EXISTING `trip` table, whose DDL lives in
// `store.go`'s `schema` const (store.go:195–228) and is therefore created by
// `store.Open` rather than by `domain.Register`. `domain.Register` is still
// called, so the artefact introspected is the fully-registered store a post
// actually runs — not a bare Open that no deployment ever has.
//
// CONTROL (`W5-X6` (a), "W5-X5's pre-bump control"): the same fixture
// machinery, stamped with the CURRENT version, must Open SUCCESSFULLY, and it
// runs FIRST. Without it a fixture that refuses for any other reason — a
// corrupted rewrite, a wrong path — reads as a pass.
// ⟦was: TestW5eColumnRidesTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12⟧
func TestW5eColumnRidesTheV12ShapeAndAV11StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL, first: the pre-bump control ------------------------------
	// A store stamped the CURRENT version must Open. This proves the fixture
	// machinery below builds an OPENABLE store, so the '9' refusal that
	// follows is about the version and not about a broken rewrite.
	ctlPath := filepath.Join(dir, "w5e-x5-control.db")
	w5eStampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W5-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v — the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) W5e's column is inside the v12 shape ------------------------
	livePath := filepath.Join(dir, "w5e-x5.db")
	st, err := store.Open(livePath)
	if err != nil {
		t.Fatalf("open %s: %v", livePath, err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatalf("register: %v", err)
	}
	stored, err := st.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("fixture defect: reading back schema_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Rule 6 — the arrangement this arm needs must actually hold: the store
	// whose version was just read must be the one carrying `cargo_note` on
	// `trip`. Without this the version read is about SOME store, not about
	// W5e's. The column literal is `w5eCargoNoteColumn` (W5e-1b's pinned
	// wire/column spelling), never a Go identifier.
	live := w5eOpenRaw(t, livePath)
	cols := w5eColumns(t, live, "trip")
	if _, ok := w5eFindColumn(cols, w5eCargoNoteColumn); !ok {
		t.Fatalf("fixture defect: the store whose schema_version was read does not carry W5e's column %q on `trip` — W5-X5's subject here is W5e's OWN shape at the window's version, and this store is not it\n  actual columns: %v", w5eCargoNoteColumn, w5eColumnNames(cols))
	}

	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W5-X5(iii): the store carrying W5e's %q column reads back schema_version %q, want %q (= fmt.Sprint(store.SchemaVersion))", w5eCargoNoteColumn, stored, want)
	}
	// Re-pinned 15 -> 16 (R3 alert_kind, 2026-10-04): this literal tracks the
	// CURRENT version. ⟦was: `stored != "15"`⟧
	if stored != "16" {
		t.Errorf("W5-X5(iii) (m5-X5, RE-POINTED AGAIN): the store carrying W5e's %q column reads back schema_version %q, want \"16\" — the 15 -> 16 increment's version (R3 alert_kind, 2026-10-04, the sole bump of fix round 2), re-pinned from the 14 -> 15 increment's EXIT version (R-20 + R-23, 2026-09-13; the rejoin path — visit carries its minting key, is_open is derived, restored presence is marked). W5e's own column rode the 9->10 window and has not moved since; this literal just tracks whatever the CURRENT version is, re-pointed at the 11->12 bump (R-16), again at 12->13 (R-17), again at 13->14 (R-18), and again here. A stray bump here silently falsifies design/WINDOW-CHECKLIST-v1.md:187 (the PRINTED operator checkbox, now stale at \"14\" until a later pass updates it too), TestW1SchemaVersionIsSixteen, and internal/repl's v14 refusal pin", w5eCargoNoteColumn, stored)
	}

	// --- (i)/(ii) a store stamped the LITERAL '11' is refused at Open ------
	// '11' is the literal predecessor — the version the schema-11 window
	// actually ran, so the only stale database a v12 binary can meet (W5-X5(ii):
	// "never SchemaVersion-1"). SchemaVersion-1 would be 11 today and would
	// silently become 12 under the mutant above, at which point this arm would
	// stop testing the edge it names. ⟦re-pinned from '10' at the 11 -> 12
	// increment, 2026-09-11⟧
	oldPath := filepath.Join(dir, "w5e-x5-v11.db")
	w5eStampSchemaVersion(t, oldPath, "11")
	if old, err := store.Open(oldPath); err == nil {
		old.Close()
		t.Errorf("W5-X5(i) (m5-X5): Open SUCCEEDED over a store stamped schema_version '11' while this binary is %d; the v12 binary must refuse it. The alternative is a v11 database silently opened by a v12 binary and a `no such column` at the first use of the increment's shape, with a truck at the gate", store.SchemaVersion)
	} else if msg := err.Error(); !strings.Contains(msg, "schema_version 11 ") || !strings.Contains(msg, fmt.Sprint(store.SchemaVersion)) || !strings.Contains(msg, "refusing") {
		// Assertion cloned from TestW1V16BinaryRefusesAV15DatabaseAtOpen
		// (w1_version_gate_test.go:36-56), NEVER a type switch:
		// store.SchemaVersionError is the per-envelope REPLICATION refusal and
		// is NOT on the Open path, so errors.As here books a kill that can
		// never be produced (W5-X5(i), stated in terms).
		t.Errorf("W5-X5(i): the refusal must be a non-nil UNTYPED error naming BOTH versions and the word \"refusing\", got: %v", err)
	}
}

// w5eStampSchemaVersion creates a store at path and rewrites its `meta`
// schema_version row to version, then closes it — W5-X5(ii)'s stated fixture
// construction ("by rewriting meta on a freshly opened store … not by obtaining
// a pre-W5 binary").
//
// ⚠️ WRITTEN FRESH, not recovered. The withdrawn helper of the same name went
// with the tombstone and this file was never committed, so git history holds
// nothing to restore. It is modelled on `w5cStampSchemaVersion`
// (w5c_projection_and_version_test.go:514) rather than invented.
func w5eStampSchemaVersion(t *testing.T, path, version string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixture defect: creating the old-store fixture: %v", err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatalf("fixture defect: registering the old-store fixture: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing the old-store fixture: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("fixture defect: raw open of %s: %v", path, err)
	}
	res, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, version)
	if err != nil {
		db.Close()
		t.Fatalf("fixture defect: stamping schema_version=%s: %v", version, err)
	}
	// Rule 6: an UPDATE touching zero rows would leave the fixture stamped
	// with the binary's own version and every assertion below vacuous.
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		db.Close()
		t.Fatalf("fixture defect: stamping schema_version=%s affected %d rows (err=%v) — the fixture did not become an old store, so a refusal below would prove nothing", version, n, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("fixture defect: closing the raw handle: %v", err)
	}
}
