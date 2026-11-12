// ⚠️ READ FIRST — W5-X5's BUMP REQUIREMENT IS STRUCK (2026-09-01).
// reports/W5-SCHEMAVERSION-ADJUDICATION.md rules that the schema-10 window
// carries exactly ONE SchemaVersion increment — W1's 9 → 10 — and that NO W5
// sub-chunk bumps it. Every mention of "the shape change bumps
// store.SchemaVersion" below is SUPERSEDED text, kept so the record stays
// diffable; the removal record further down this file states what went and why.
// The criteria this file still carries live — W5-X4 and its controls — are
// UNAFFECTED.

// w5b_projection_and_version_test.go — W5b's STORE-LEVEL halves:
//
//   - W5b-0 (schema half) in W5-X4's two-part form: the `person` projection
//     gains a NULLABLE `TEXT phone` with no default, STRICT preserved, and NO
//     column outside the permitted union.
//   - W5-X5: the projection SHAPE CHANGE bumps store.SchemaVersion, proved by
//     an open-an-old-store test with its pre-bump CONTROL.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31,
// sub-chunk W5b (item 2 — the personal phone).
//
// package store_test DELIBERATELY (w3_sidetable_structural_test.go's reason,
// restated because it is load-bearing): these assertions need domain.Register
// to create the projection, and `package store` cannot import internal/domain
// — domain imports store. This file references NO unbuilt Go symbol, so it
// COMPILES AND RUNS against `88dd6a6` and its clean-tree failures are genuine
// PRODUCED RUNTIME REDS, not compile errors.
//
// EXPECTED RED at 88dd6a6 (both tests, runtime):
//   - TestW5bPersonProjectionPhoneColumnW5X4: "`person` is missing its
//     declared column [phone]" — the projection is still the pre-chunk six
//     (refdata.go:197–204, STRICT).
//   - ⟦was: "TestW5bSchemaVersionBumpedForThePersonPhoneShapeChange"⟧:
//     "store.SchemaVersion is 10" — the shape change has not been declared.
//
// KILLER MUTANTS:
//
//	(m5b-5)  `person.phone` dropped, renamed or declared NOT NULL; or the
//	         fold silently not writing it              -> part (a) below
//	(m5-X4)  a column outside the permitted union added and not caught
//	                                                   -> part (b) below
//	(m5-X5)  a column added with no SchemaVersion bump: green at Open, red at
//	         the first enrolment                       -> the version test
//
// CONTROLS (W5-X6 (a): W5b-0 introspection, W5-X4 both parts, W5-X5's
// pre-bump control). Every detector is shown detecting a PLANTED violation on
// a scratch table BEFORE the real table's clean pass counts — one plant per
// tooth, so a detector with only one tooth left cannot pass silently.
package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- SEAMS bound here, once; disputes go to the gate --------------------
//
// SEAM W5b-S-COL: the new `person` column is spelled `phone`. NOT a free
// choice — W5-X4 part (b) names it verbatim ("`phone` (W5b)"), and it is the
// spelling W3 already landed on `person_amendment` (person_amend.go:145).
// Recorded as a seam only because this file asserts it as a literal.
//
// SEAM W5b-S-PREW5: the pre-W5 SchemaVersion is the literal 10
// (store.go:102 at 88dd6a6) [MEASURED]. W5-X5(ii)'s "the LITERAL predecessor
// — '10' — never SchemaVersion-1".

const w5bPreW5SchemaVersion = 10

// w5bPersonPreChunkColumns is the MEASURED pre-chunk `person` column set
// (refdata.go:197–204 at 88dd6a6) — W5-X4 part (b)'s base.
var w5bPersonPreChunkColumns = []string{
	// ⟦LOCK — §82, 2026-09-03⟧ ⭐ THE PRE-CHUNK BASE IS RE-POINTED, NOT DISCHARGED.
	// ⟦was: "\t\"person_id\", \"site_id\", \"name\", \"company_id\", \"workgroup_id\", \"recorded_by\","⟧
	//
	// ⚠ THIS TEST'S OWN ASSERTION WAS NEVER BROKEN. It failed at the Rule 6
	// arrangement guard above/below — before its subject ran — because W8
	// renamed `person.workgroup_id` to `person.trade_id` (DR-W8-2 / P-3, and
	// the rename is sanctioned). [MEASURED, S82-W5-CLUSTER §D2] with the base
	// retargeted and NOTHING else changed, this test PASSES. Its subject is
	// intact; only the literal it measures against had gone stale.
	//
	// ⛔ SO IT IS NOT DISCHARGED. §82 offers two dispositions — a LOCK-TIME
	// criterion is discharged, a STANDING one the rename BROKE is a W8 defect.
	// This is neither: the assertion still holds and still has work to do.
	// Discharging it would delete live coverage to tidy away a stale literal.
	//
	// ⭐ THE BASE IS SCAFFOLDING, THE ARRANGEMENT IS THE CRITERION ⟦§90⟧: what
	// this list carries is "the six columns `person` had before W5, unchanged
	// underneath W5's additions" — six columns, one of them the trade/workgroup
	// reference. It does not carry "that column is spelled `workgroup_id`".
	// `trade_id` is the DIRECT SUCCESSOR AT THE SAME POSITION ⟦§61⟧: at 88dd6a6
	// `PersonCreated` field 5 was `WorkgroupID string `+"`"+`json:"workgroup_id"`+"`"+`;
	// today it is `TradeID string `+"`"+`json:"trade_id"`+"`"+` in the same slot.
	"person_id", "site_id", "name", "company_id", "trade_id", "recorded_by",
}

// w5bDeclaredAddition is W5b's own declared addition — the column part (a)
// REQUIRES.
const w5bDeclaredAddition = "phone"

// w5bEarlierSubChunkColumns are W5a's declared additions — earlier-ordered
// under W5-X3 (W5a → W5b → W5c). They are PERMITTED here, never required:
// W5b gates alone, whether or not W5a has landed.
var w5bEarlierSubChunkColumns = []string{"last_name", "first_name", "position"}

// w5bLaterSubChunkAdditions are the `person` columns a LATER-ordered
// sub-chunk declares. Under W5-X4 part (b) as amended they are INSIDE the
// permitted union, on the same terms as W5a's three: part (b)'s union is W5's
// full declared addition set, not a landing-order prefix.
// ⟦SchemaVersion 11, 2026-09-05⟧ EMPTY: W5c's `pad_oriented`, the only
// later-ordered addition, is STRUCK ([[D-88]] ERRATA / [[D-91]]). It leaves
// the union, so its return is a STRAY part (b) reds on by name — the control
// below now plants it as one. ⟦was: []string{"pad_oriented"}⟧
var w5bLaterSubChunkAdditions = []string{}

// w5bLaterSubChunkColumns names the owner of each later-ordered column, for
// the message-formatting branch below. Under the amended union that branch is
// unreachable by construction. It is KEPT rather than deleted, on the
// amendment's own instruction: it records what the reading used to be, and if
// it ever fires it means w5bPermittedUnion has drifted from W5-X4's
// enumeration sentence — a defect in this file, not a serialisation violation.
var w5bLaterSubChunkColumns = map[string]string{} // ⟦was: {"pad_oriented": "W5c"} — struck at SchemaVersion 11⟧

// w5bPermittedUnion is W5-X4 part (b)'s permitted union: the pre-chunk six
// PLUS W5's declared additions — the ELEVEN columns the enumeration sentence
// names, in that order, with NO scoping by landing order.
//
// ⟦LOCK — W5a-G-2, post-lock amendment 2026-09-01. THE TIGHTENING IS STRUCK.⟧
// This file previously built the TEN-column tightened union ("this sub-chunk
// and every earlier-ordered sub-chunk"), which reds on W5c's `pad_oriented`.
// That reading was ruled a defect in the CRITERION — not in W5b's build, whose
// `phone` passes part (a), and not in this test, which implemented the
// operative reading correctly and loudly. The tightened union is a statement
// about the WHOLE TREE's columns, which coincides with a statement about THIS
// sub-chunk's bytes only if each sub-chunk is gated before the next one lands,
// and nothing in the plan ever required that: all three person sub-chunks
// built into one uncommitted working tree, so the row was unsatisfiable by
// construction at W5b's own gate and unrepairable there. Authority:
// reports/window-build/W5a-falsification.md §2.2 and §9's G-2, adopted for
// this file by W5b-falsification.md §3.1 and §12; the amendment and its
// measured discharge are at reports/W5-POSTLOCK-AMENDMENTS.md §15.2 / §16.3 /
// §18.
//
// ⚠ WHAT PART (b) NO LONGER CARRIES: anti-OVER-build. Anti-SUBSTITUTION —
// W5b's row cannot go green on another sub-chunk's column — is carried by part
// (a) below, which names `phone` as a literal; that is where (m5b-5) bites.
// ⟦was: "part (b)'s permitted set under the tightening" — pre-chunk six +
// w5bEarlierSubChunkColumns + `phone`, i.e. ten⟧
var w5bPermittedUnion = func() []string {
	out := append([]string{}, w5bPersonPreChunkColumns...)
	out = append(out, w5bEarlierSubChunkColumns...)
	out = append(out, w5bDeclaredAddition)
	return append(out, w5bLaterSubChunkAdditions...)
}()

// --- raw introspection machinery ----------------------------------------

func w5bOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// w5bNewRegisteredStore opens a store with every domain projection created.
func w5bNewRegisteredStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "w5b.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	return st, path
}

type w5bColumn struct {
	Name     string
	Type     string
	NotNull  bool
	HasDflt  bool
	DfltText string
}

func w5bTableInfo(t *testing.T, db *sql.DB, table string) []w5bColumn {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []w5bColumn
	for rows.Next() {
		var c w5bColumn
		var nn int
		var dflt sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &nn, &dflt); err != nil {
			t.Fatal(err)
		}
		c.NotNull = nn != 0
		c.HasDflt = dflt.Valid
		c.DfltText = dflt.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("fixture defect: pragma_table_info(%q) returned no rows — the table does not exist, so every assertion over it would be vacuous", table)
	}
	return out
}

func w5bColumnNames(cols []w5bColumn) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func w5bFindColumn(cols []w5bColumn, name string) (w5bColumn, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return w5bColumn{}, false
}

// w5bOutsideUnion returns the columns present that the permitted union does
// not name.
func w5bOutsideUnion(cols []w5bColumn, permitted []string) []string {
	allowed := map[string]bool{}
	for _, p := range permitted {
		allowed[p] = true
	}
	var out []string
	for _, c := range cols {
		if !allowed[c.Name] {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

// w5bShapeFaults reports every way the named column departs from "nullable
// TEXT, no default". It returns a LIST rather than a bool so a single relaxed
// tooth cannot hide behind another's pass.
func w5bShapeFaults(cols []w5bColumn, name string) []string {
	c, ok := w5bFindColumn(cols, name)
	if !ok {
		return []string{"absent"}
	}
	var faults []string
	if !strings.EqualFold(c.Type, "TEXT") {
		faults = append(faults, fmt.Sprintf("declared type %q, want TEXT", c.Type))
	}
	if c.NotNull {
		faults = append(faults, "declared NOT NULL — the column must stay NULLABLE; mandatoriness is an EDGE property of one handler (D-52), never a storage property")
	}
	if c.HasDflt {
		faults = append(faults, fmt.Sprintf("carries a DEFAULT (%s) — a default turns \"no phone\" into a fabricated value and hides the gap D-52 accepts", c.DfltText))
	}
	return faults
}

var w5bStrictRe = regexp.MustCompile(`(?is)\)\s*strict\s*;?\s*$`)

func w5bTableDDL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		t.Fatalf("reading DDL for %s: %v", table, err)
	}
	return ddl
}

// w5bStrictBehaviourHolds is the BEHAVIOURAL half of "STRICT preserved": in a
// STRICT table a BLOB cannot be stored in a TEXT column. A declaration regex
// alone is satisfied by the word STRICT appearing in a comment; this is not.
//
// ⚠ The probe value is a BLOB deliberately: TEXT *affinity* silently converts
// a numeric value to text, so an INTEGER probe is accepted by a STRICT table
// and would report a STRICT table as loose. The control below proves the
// discrimination rather than asserting it.
func w5bStrictBehaviourHolds(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(
		`INSERT INTO %s (person_id, site_id, name) VALUES ('w5b-strict-probe', 'site-a', x'00ff')`, table))
	return err != nil
}

// =======================================================================
// W5b-0's schema half, in W5-X4's two-part form
// =======================================================================

// TestW5bPersonProjectionPhoneColumnW5X4 — W5b-0's introspection half.
//
// ⚠ WHY THIS TEST EXISTS AT ALL, in the lock's own words: W5b builds three
// things, and measured at rev 6 **two of the three had no criterion that
// could fail on them** — "so W5b could complete a green ledger having built
// the refusal and neither the payload field nor the column". W5a had an
// introspection criterion and a dropped-column mutant; W5c had both; W5b had
// neither. This test and its domain twin are that hole closed.
//
// EXPECTED RED (pre-chunk, genuine, unaided): part (a) fails with `phone`
// absent — the `person` projection is the pre-chunk six at 88dd6a6.
// EXPECTED RED (m5b-5): drop or rename the column, or declare it NOT NULL, or
// give it a default -> part (a). EXPECTED RED (m5-X4): add any column outside
// the permitted union -> part (b).
func TestW5bPersonProjectionPhoneColumnW5X4(t *testing.T) {
	st, path := w5bNewRegisteredStore(t)
	_ = st
	db := w5bOpenRaw(t, path)

	// ---------------------------------------------------------------
	// CONTROLS FIRST. Every tooth is shown biting a PLANTED violation on
	// scratch tables before the real table is read. Without this, a clean
	// pass over `person` is indistinguishable from a detector that cannot
	// detect (standing rule 5).
	// ---------------------------------------------------------------
	type plant struct {
		name string
		ddl  string
		want string // the fault the detector must report
	}
	plants := []plant{
		{"w5b_ctl_missing", `CREATE TABLE w5b_ctl_missing (
			person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
			company_id TEXT, trade_id TEXT, recorded_by TEXT) STRICT;`, "absent"},
		{"w5b_ctl_notnull", `CREATE TABLE w5b_ctl_notnull (
			person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
			company_id TEXT, trade_id TEXT, recorded_by TEXT, phone TEXT NOT NULL) STRICT;`, "NOT NULL"},
		{"w5b_ctl_default", `CREATE TABLE w5b_ctl_default (
			person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
			company_id TEXT, trade_id TEXT, recorded_by TEXT, phone TEXT DEFAULT '000') STRICT;`, "DEFAULT"},
		{"w5b_ctl_wrongtype", `CREATE TABLE w5b_ctl_wrongtype (
			person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
			company_id TEXT, trade_id TEXT, recorded_by TEXT, phone INTEGER) STRICT;`, "want TEXT"},
		{"w5b_ctl_renamed", `CREATE TABLE w5b_ctl_renamed (
			person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
			company_id TEXT, trade_id TEXT, recorded_by TEXT, phone_number TEXT) STRICT;`, "absent"},
	}
	for _, p := range plants {
		if _, err := db.Exec(p.ddl); err != nil {
			t.Fatalf("CONTROL setup: creating %s: %v", p.name, err)
		}
		faults := w5bShapeFaults(w5bTableInfo(t, db, p.name), w5bDeclaredAddition)
		if len(faults) == 0 {
			t.Fatalf("CONTROL FAILED (part (a)): the planted violation %s was reported CLEAN — the shape detector has no tooth for %q, so the real table's pass below would prove nothing (m5b-5)", p.name, p.want)
		}
		if !strings.Contains(strings.Join(faults, "; "), p.want) {
			t.Fatalf("CONTROL FAILED (part (a)): %s reported %v, which does not name %q — the detector fires for the wrong reason", p.name, faults, p.want)
		}
	}

	// Part (b)'s tooth, planted, and RE-AIMED by post-lock amendment §15.2's
	// corrected discharge action (§16.3). The scratch table carries BOTH an
	// invented column (`nickname`) and W5c's declared `pad_oriented`, so this
	// one plant is now a control in BOTH directions: the detector must report
	// the invented column, and must NOT report a column that is inside W5's
	// declared additions. Widening the union without re-aiming this assertion
	// leaves the test red on its own control — which is how a pass applying the
	// amendment's ORIGINAL wording verbatim would arrive at a CONTROL failure
	// while holding a document promising that controls do not change.
	// ⟦was: want [nickname pad_oriented], under the STRUCK tightening⟧
	// ⟦SchemaVersion 11, 2026-09-05⟧ ⟦was: want [nickname] — `pad_oriented`
	// permitted⟧ The plant now carries an invented column AND the STRUCK
	// column, and BOTH must be reported: `pad_oriented` is no longer in
	// anyone's union (D-88 ERRATA), so a detector that lets it back in is the
	// stale reading a third time over.
	if _, err := db.Exec(`CREATE TABLE w5b_ctl_extra (
		person_id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL,
		company_id TEXT, trade_id TEXT, recorded_by TEXT, phone TEXT,
		nickname TEXT, pad_oriented TEXT) STRICT;`); err != nil {
		t.Fatalf("CONTROL setup: %v", err)
	}
	outside := w5bOutsideUnion(w5bTableInfo(t, db, "w5b_ctl_extra"), w5bPermittedUnion)
	if len(outside) != 2 || outside[0] != "nickname" || outside[1] != "pad_oriented" {
		t.Fatalf("CONTROL FAILED (part (b)): the planted out-of-union columns were reported as %v, want [nickname pad_oriented] — part (b) is the only tooth for (m5-X4). It must see the INVENTED column AND the STRUCK one (pad_oriented left every union at SchemaVersion 11); a detector that misses either proves nothing", outside)
	}

	// The STRICT probe's own control: it must call a NON-strict table loose.
	if _, err := db.Exec(`CREATE TABLE w5b_ctl_loose (
		person_id TEXT PRIMARY KEY, site_id TEXT, name TEXT, phone TEXT);`); err != nil {
		t.Fatalf("CONTROL setup: %v", err)
	}
	if w5bStrictBehaviourHolds(t, db, "w5b_ctl_loose") {
		t.Fatalf("CONTROL FAILED: the STRICT probe reported a NON-STRICT table as strict — every \"STRICT preserved\" pass below would be true by construction")
	}

	// ---------------------------------------------------------------
	// PART (a) — W5b's own column, on the real table.
	// ---------------------------------------------------------------
	cols := w5bTableInfo(t, db, "person")
	if faults := w5bShapeFaults(cols, w5bDeclaredAddition); len(faults) > 0 {
		t.Errorf("W5-X4(a) / W5b-0 (m5b-5): the `person` projection's %q column: %s\n  actual columns: %v\n  W5b builds three things — the PersonCreated half, the person.phone column, and the enrolment refusal. This is the column.",
			w5bDeclaredAddition, strings.Join(faults, "; "), w5bColumnNames(cols))
	}

	// STRICT preserved — declaration AND behaviour.
	ddl := w5bTableDDL(t, db, "person")
	if !w5bStrictRe.MatchString(strings.TrimSpace(ddl)) {
		t.Errorf("W5-X4(a) / W5b-0: the `person` table's DDL does not end in STRICT: %s", ddl)
	}
	if !w5bStrictBehaviourHolds(t, db, "person") {
		t.Errorf("W5-X4(a) / W5b-0: the `person` table accepted a BLOB into a TEXT column — STRICT is declared but not in force. A widened table re-created without STRICT loses the type discipline every projection in this store depends on")
	}

	// ---------------------------------------------------------------
	// PART (b) — no column outside the permitted union.
	// ---------------------------------------------------------------
	if outside := w5bOutsideUnion(cols, w5bPermittedUnion); len(outside) > 0 {
		var notes []string
		for _, c := range outside {
			if owner, ok := w5bLaterSubChunkColumns[c]; ok {
				notes = append(notes, fmt.Sprintf("%s (declared by %s and INSIDE part (b)'s eleven-column union — reaching this branch means w5bPermittedUnion has drifted from W5-X4's enumeration sentence, a defect in this test file rather than in the tree)", c, owner))
				continue
			}
			notes = append(notes, c+" (outside W5's declared additions entirely)")
		}
		t.Errorf("W5-X4(b) (m5-X4): the `person` projection carries %s.\n  permitted union (W5-X4(b) as amended 2026-09-01: the pre-chunk six + W5's declared additions, no scoping by landing order) = %v\n  actual = %v",
			strings.Join(notes, ", "), w5bPermittedUnion, w5bColumnNames(cols))
	}

	// Rule 6 — the arrangement: the pre-chunk six must still be there. Part
	// (b) only reports columns that should NOT be present; a build that
	// DELETED half the table would satisfy it silently.
	for _, want := range w5bPersonPreChunkColumns {
		if _, ok := w5bFindColumn(cols, want); !ok {
			t.Errorf("W5-X4: the pre-chunk column %q is GONE from `person` — part (b) reports additions, not deletions, so this arm is what stops a widened table being rebuilt narrower", want)
		}
	}
}

// =======================================================================
// W5-X5 — STRUCK. The shape change does NOT bump store.SchemaVersion;
// it rides W1's single 9 → 10 (W5-SCHEMAVERSION-ADJUDICATION.md, 2026-09-01).
// The fixture machinery below is retained; its test was removed.
// =======================================================================

// w5bStampSchemaVersion creates a store at `path`, closes it, and rewrites
// `meta.schema_version` to `version` — W5-X5(ii)'s fixture construction ("by
// rewriting `meta` on a freshly opened store … NOT by obtaining a pre-W5
// binary").
func w5bStampSchemaVersion(t *testing.T, path, version string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("fixture defect: opening %s: %v", path, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing %s: %v", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("fixture defect: raw open of %s: %v", path, err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, version)
	if err != nil {
		t.Fatalf("fixture defect: stamping schema_version=%s: %v", version, err)
	}
	// Rule 6: an UPDATE touching zero rows leaves the fixture stamped with
	// the binary's own version and every assertion below vacuous.
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fixture defect: stamping schema_version=%s touched %d rows, want 1", version, n)
	}
}

// w5bLandedPersonSubChunks counts how many of W5a/W5b/W5c have landed a
// `person` shape change, READ OFF THE TABLE rather than assumed. It is the
// input to the only arithmetic in this file that is order-independent.
func w5bLandedPersonSubChunks(cols []w5bColumn) (n int, which []string) {
	has := func(name string) bool { _, ok := w5bFindColumn(cols, name); return ok }
	if has("last_name") || has("first_name") || has("position") {
		n, which = n+1, append(which, "W5a")
	}
	if has(w5bDeclaredAddition) {
		n, which = n+1, append(which, "W5b")
	}
	// ⟦SchemaVersion 11⟧ W5c's column is struck; its arm is gone from this
	// caller-less arithmetic too. ⟦was: if has("pad_oriented") { … "W5c" }⟧
	return n, which
}

// ── REMOVED by reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01) ──────────
// ⟦was: "TestW5bSchemaVersionBumpedForThePersonPhoneShapeChange"⟧
// asserted a WITHIN-WINDOW `store.SchemaVersion` bump for W5b. That
// requirement is STRUCK. The schema-10 window carries exactly ONE increment —
// W1's 9 → 10 at 49abf65 — and every W5 sub-chunk rides it: the v10 log starts
// empty (D-29), §7 step 2 deletes every database at the cutover, and one
// hash-verified binary ships to both posts, so no stored database and no peer
// meets a differently-shaped binary inside the window (plan §8 bump-gate rows
// 3, 8, 10 price these columns as "window-priced"). The error was in W5-X5 AS
// LOCKED, not introduced by post-lock amendment §1, which propagated it.
// Its arm (0b) — `SchemaVersion >= 10 + landed`, counting landed person
// sub-chunks off the table — and its arm (ii-b) derived predecessor existed
// ONLY to enforce per-sub-chunk bumping. Its own failure text routed this to
// the gate: "IF THIS IS A FALSE RED: it means [these] landed under a SINGLE
// shared bump … Take it to the gate rather than relaxing it." This is that
// gate, and the answer is that it IS a false red.
// W5b's real column protection is W5-X4, which introspects the landed
// column set and is UNAFFECTED by this ruling — see
// `TestW5bPersonProjectionPhoneColumnW5X4` in this file.
// Removed by the plan-owner pass, not by a builder (the route by which
// muster_test.go's live half was amended, post-lock amendments §5/§6).
// ─────────────────────────────────────────────────────────────────────────────

// w5bRequireOpenRefuses asserts the W5-X5(i) refusal shape: a NON-NIL,
// UNTYPED error whose message names BOTH versions and the word "refusing".
//
// ⚠ It clones `TestW1V16BinaryRefusesAV15DatabaseAtOpen`'s assertion
// (w1_version_gate_test.go:36–56), NOT a type switch. `store.SchemaVersionError`
// is NOT on the Open path: it is the per-envelope REPLICATION refusal,
// declared at envelope.go:170–174 and constructed only at log.go:202. A
// criterion naming it books a kill no errors.As on this path can produce.
func w5bRequireOpenRefuses(t *testing.T, path, stored, label string) {
	t.Helper()
	st, err := store.Open(path)
	if err == nil {
		st.Close()
		t.Errorf("%s (m5-X5): Open SUCCEEDED over a store stamped schema_version %s while this binary is %d. A store whose shape predates this build must be refused AT OPEN — the alternative is a clean start and a failure at the first enrolment, with 'no such column' and a person standing at the window", label, stored, store.SchemaVersion)
		return
	}
	msg := err.Error()
	for _, want := range []string{stored, fmt.Sprint(store.SchemaVersion), "refusing"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%s: Open refused, but its message does not contain %q: %q. The refusal must name BOTH versions and say it is refusing, or an operator cannot tell a version mismatch from a corrupt file", label, want, msg)
		}
	}
}

// =======================================================================
// W5-X5 at W5b — THE ROW, LANDED.
// ⟦LOCK — W5b's gate G-1, closed by a plan-owner pass 2026-09-01, on the
// pattern post-lock amendment §15.3 landed for W5a.⟧
//
// The removal record above is CORRECT and stands: the WITHIN-WINDOW BUMP
// requirement is struck and W5b does not bump. What §15.3 then re-pointed
// W5-X5 to is a DIFFERENT assertion — the '9' edge — and W5b owes it. Its
// gate produced (m5-X5) (`const SchemaVersion = 10` -> `11`) and measured
// that NO W5b test detected it: the row was a BLANK, which §15.3 forbids in
// terms ("recorded as a ruling, never left blank"). This is that row.
// =======================================================================

// TestW5bColumnRidesTheV12ShapeAndAV11StoreIsRefused is W5b's OWN W5-X5 row.
// W5-X5's three-part form, with W5b's column as the subject:
//
//	(iii) a store that carries W5b's `phone` column reads back
//	      StoredSchemaVersion() == fmt.Sprint(store.SchemaVersion) AND == "12"
//	      ⟦re-pinned 2026-09-11 at the 11 -> 12 increment (R-16); was "11"⟧
//	      — the window's exit version, pinned. THIS IS THE KILLING ARM.
//	(i)   a store stamped the literal predecessor '11' is refused at Open by a
//	      non-nil UNTYPED error naming both versions and the word "refusing".
//	(ii)  the fixture stamps the LITERAL '11', never store.SchemaVersion-1.
//
// KILLER MUTANT: (m5-X5) as RE-POINTED — "a sub-chunk that BUMPS
// SchemaVersion". Under it store.SchemaVersion is 12, the store W5b's column
// lands in stamps "12", and arm (iii) reds. Arms (i)/(ii) stay green under
// that mutant (a '9' store is refused by a 12 binary too), which is exactly
// why (iii) is pinned to a LITERAL and not only to a relation — an unpinned
// relation-only read-back is vacuous, as W5-X5 says in terms.
//
// ⚠️ NOT A COPY OF W1's TESTS, and not a borrow of W5a's.
// TestW1SchemaVersionIsSixteen reads the CONSTANT; this reads the version STAMPED
// IN THE STORE THAT CARRIES `phone`, so the column shape and the version are
// asserted of the SAME artefact. W5a's landed instance makes that assertion
// of W5a's three columns, and W5-X3 ("no sub-chunk's green counts toward
// another's") forbids reading it as W5b's — the same reasoning that forbids
// reading another CHUNK's green, which is why W1's tests do not discharge
// this row either. W5b's gate ruled exactly that (§4) and refused to close
// the row by writing the test it would then bless.
//
// CONTROL (W5-X6 (a), "W5-X5's pre-bump control"): the same fixture
// machinery, stamped with the CURRENT version, must Open SUCCESSFULLY, and it
// runs FIRST. Without it a fixture that refuses for any other reason — a
// corrupted rewrite, a wrong path — reads as a pass. W5b's gate booked this
// control as ❌ NOT SERVED (§5, C13) because there was nothing to attach it
// to; it is served here.
//
// ⚠️ w5bLandedPersonSubChunks stays CALLER-LESS on purpose. Its arithmetic
// was the input to the STRUCK per-sub-chunk bump arm (0b), and nothing here
// resurrects it: this row asserts a pinned literal, not a count.
// ⟦was: TestW5bColumnRidesTheV11ShapeAndAV10StoreIsRefused — re-pinned 11 -> 12⟧
func TestW5bColumnRidesTheV12ShapeAndAV11StoreIsRefused(t *testing.T) {
	dir := t.TempDir()

	// --- CONTROL, first: the pre-bump control ------------------------------
	// A store stamped the CURRENT version must Open. This proves the fixture
	// machinery below builds an OPENABLE store, so the '9' refusal that
	// follows is about the version and not about a broken rewrite.
	ctlPath := filepath.Join(dir, "w5b-x5-control.db")
	w5bStampSchemaVersion(t, ctlPath, fmt.Sprint(store.SchemaVersion))
	ctl, err := store.Open(ctlPath)
	if err != nil {
		t.Fatalf("W5-X5 CONTROL FAILED (pre-bump control): a store stamped the CURRENT version %d did not Open: %v — the fixture rewrite is broken, so the '9' refusal below would prove nothing", store.SchemaVersion, err)
	}
	if err := ctl.Close(); err != nil {
		t.Fatal(err)
	}

	// --- (iii) W5b's column is inside the v10 shape ------------------------
	livePath := filepath.Join(dir, "w5b-x5.db")
	st, err := store.Open(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	stored, err := st.StoredSchemaVersion()
	if err != nil {
		t.Fatalf("fixture defect: reading back schema_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Rule 6 — the arrangement this arm needs must actually hold: the store
	// whose version was just read must be the one carrying `phone`. Without
	// this the version read is about SOME store, not about W5b's.
	live := w5bOpenRaw(t, livePath)
	cols := w5bTableInfo(t, live, "person")
	if _, ok := w5bFindColumn(cols, w5bDeclaredAddition); !ok {
		t.Fatalf("fixture defect: the store whose schema_version was read does not carry W5b's column %q — W5-X5's subject here is W5b's OWN shape at the window's version, and this store is not it\n  actual columns: %v", w5bDeclaredAddition, w5bColumnNames(cols))
	}

	if want := fmt.Sprint(store.SchemaVersion); stored != want {
		t.Errorf("W5-X5(iii): the store carrying W5b's %q column reads back schema_version %q, want %q (= fmt.Sprint(store.SchemaVersion))", w5bDeclaredAddition, stored, want)
	}
	// Re-pinned 15 -> 16 (R3 alert_kind, 2026-10-04): this literal tracks the
	// CURRENT version. ⟦was: `stored != "15"`⟧
	if stored != "16" {
		t.Errorf("W5-X5(iii) (m5-X5, RE-POINTED AGAIN): the store carrying W5b's %q column reads back schema_version %q, want \"16\" — the 15 -> 16 increment's version (R3 alert_kind, 2026-10-04, the sole bump of fix round 2), re-pinned from the 14 -> 15 increment's EXIT version (R-20 + R-23, 2026-09-13; the rejoin path — visit carries its minting key, is_open is derived, restored presence is marked). W5b's own column rode the 9->10 window and has not moved since; this literal just tracks whatever the CURRENT version is, re-pointed at the 11->12 bump (R-16), again at 12->13 (R-17), again at 13->14 (R-18), and again here. A stray bump here silently falsifies design/WINDOW-CHECKLIST-v1.md:171 (the PRINTED operator checkbox, now stale at \"14\" until a later pass updates it too), TestW1SchemaVersionIsSixteen, and internal/repl's v14 refusal pin", w5bDeclaredAddition, stored)
	}

	// --- (i)/(ii) a store stamped the LITERAL '11' is refused at Open ------
	// '11' is the literal predecessor — the version the schema-11 window
	// actually ran, so the only stale database a v12 binary can meet (W5-X5(ii):
	// "never SchemaVersion-1"). SchemaVersion-1 would be 11 today and would
	// silently become 12 under the mutant above, at which point this arm would
	// stop testing the edge it names. ⟦re-pinned from '10' at the 11 -> 12
	// increment, 2026-09-11⟧
	//
	// SEAM W5b-S-PREW5 above records the pre-W5 literal as 10 and is SUPERSEDED
	// by the adjudication: 10 was the schema-10 window's EXIT version. At the
	// 11 -> 12 increment the literal PREDECESSOR is 11, and it is stamped
	// as a literal here.
	oldPath := filepath.Join(dir, "w5b-x5-v11.db")
	w5bStampSchemaVersion(t, oldPath, "11")
	w5bRequireOpenRefuses(t, oldPath, "11", "W5-X5(i)")
}
