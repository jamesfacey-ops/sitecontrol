// w8_absences_test.go — W8 (item 10, the trade register): the four
// PROHIBITIONS (W8-2), the four-table projection column assertion (W8-X4),
// and the `person` half of D-66's created-state trade (W8-23(a)).
//
// Written from the LOCKED W8 criteria only. No built implementation was read
// — W8 is unbuilt at the tree this file was written against.
//
// ⚠⚠ THE INTROSPECTION FORM IS REQUIRED, NOT PREFERRED. W8's §2.7, in terms:
// "W8-2 must be written so it is a RUNTIME red, not a compile error. Written
// over Go symbols (domain.Trade) it will not compile; written over TABLE
// NAMES AS STRINGS through SQLite introspection it reds at f33c4c7 with 'no
// such table: trade'. The runtime form is REQUIRED, for the reason W6's lock
// states in terms: for a greenfield chunk the produced runtime red is the
// ONLY proof an absence assertion is anchored to the built artefact — it
// distinguishes BUILT WITHOUT PARTS from NOT BUILT. Absent that, 'no kind
// column' is satisfied by a chunk that built nothing."
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- DR-W8-4's two column sets, pinned as literals -------------------------

// w8TradeColumns is DR-W8-4: `trade` has EXACTLY these seven columns.
// [DOC — PLAN-SEC-A-schema.md:903-915]
var w8TradeColumns = []string{
	"trade_id", "site_id", "name", "recorded_by", "phone_day", "phone_night", "phone_shared",
}

// w8TradeAmendmentColumns is DR-W8-4: `trade_amendment` has EXACTLY these
// eleven, by the clone rule applied to the measured company_amendment shape.
// ⚠ NO `recorded_by` ⟦DR-W8-5⟧ — and the divergence from `trade_created`,
// which DOES carry one, is DECLARED rather than silent: "a scan that forbids
// the literal recorded_by 'everywhere on trade' is wrong on the created half".
// ⟦SchemaVersion 11, 2026-09-06⟧ `seen` is ADDED ([[D-92]], step 3 of the
// increment — the causal reference cloned onto trade second, after company):
// the author's seen-list, an immutable reference written at authoring, not a
// status. The clone rule still holds: company_amendment gained the same
// column first. ⟦was: "EXACTLY these ten" — "…\"same_as\", \"cleared\","⟧
var w8TradeAmendmentColumns = []string{
	"site_id", "node_epoch", "seq", "trade_id", "name",
	"phone_day", "phone_night", "phone_shared", "same_as", "cleared", "seen",
}

// w8PersonColumnsAfterW8 is W8-X4's `person` row and W8-23(a)'s second
// instrument: the set LOSES `workgroup_id` and GAINS `trade_id`.
//
// ⚠⚠ THE EXACT SET IS ASSERTED, NEVER ITS SIZE. [MEASURED, §2.9] `person`
// loses one column and gains one — ELEVEN BEFORE AND ELEVEN AFTER — "so a
// count-shaped assertion passes straight through the change". This is the
// same arithmetic that hides the registry's net-zero swap at §2.4.
// ⟦SchemaVersion 11, 2026-09-05⟧ `pad_oriented` is STRUCK from `person`
// ([[D-88]] ERRATA / [[D-91]]) — TEN columns now. The set is still asserted
// as a SET, never a size; the struck column coming back reds here by name.
// ⟦was: "…, \"phone\", \"pad_oriented\","⟧
var w8PersonColumnsAfterW8 = []string{
	"person_id", "site_id", "name", "company_id", "trade_id",
	"recorded_by", "last_name", "first_name", "position", "phone",
}

// w8VisitColumnsAfterW8 is W8-X4's `visit` row: the two workgroup snapshot
// columns are RENAMED to the trade spellings. ⚠ This collides with W6-7(a)'s
// LOCKED exact-set pin and is routed as SEAM-W8-4 — the collision is the plan
// owner's to amend, and this test asserts the post-W8 shape the criterion
// pins, not the amendment.
var w8VisitColumnsAfterW8 = []string{
	"visit_id", "site_id", "person_id", "trip_id",
	"company_snapshot_id", "company_snapshot_name",
	"trade_snapshot_id", "trade_snapshot_name",
	"person_snapshot_name", "is_open", "recorded_by",
	// SchemaVersion 12 (R-16): the import grandfathering marker (store.go's
	// column comment). CAPACITY ONLY — unwritten by any live authoring edge.
	"predates_orientation_rule",
	// SchemaVersion 15 (R-20): the minting envelope's key, carried so
	// is_open can be DERIVED order-independently (store.go's column comment;
	// domain/visit_open.go). Two columns, one fact — a store.Key without
	// its site limb, which is site_id above.
	"node_epoch", "seq",
}

// --- introspection primitives, over TABLE NAMES GIVEN AS STRINGS -----------

func w8Columns(t *testing.T, st *store.Store, table string) []string {
	t.Helper()
	var cols []string
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			cols = append(cols, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading pragma_table_info(%q): %v", table, err)
	}
	return cols
}

// w8TableNames is sqlite_master's set, excluding only sqlite_*.
//
// ⚠ THE TABLE SET IS sqlite_master's, NOT domain.Register()'s, and the choice
// is load-bearing ⟦LOCK — B-21⟧: [MEASURED] SIX tables exist outside
// Register() — meta, event_log, watermark, alert_ack, deadline_capture,
// deadline_red_fired — "so a table that folds nothing and registers nowhere
// passes a projection-scoped limb". A `trade_crew` added to store.go's schema
// literal is INSIDE this subject and outside that one.
func w8TableNames(t *testing.T, st *store.Store) []string {
	t.Helper()
	var names []string
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			names = append(names, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading sqlite_master: %v", err)
	}
	return names
}

// w8PrimaryKeyColumns returns the columns of a table's PRIMARY KEY, in key
// order — pragma_table_info's `pk` field, which is 0 for a non-key column and
// 1..N for the key's position.
func w8PrimaryKeyColumns(t *testing.T, st *store.Store, table string) []string {
	t.Helper()
	type kc struct {
		name string
		pos  int
	}
	var got []kc
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name, pk FROM pragma_table_info(?)`, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			var pk int
			if err := rows.Scan(&n, &pk); err != nil {
				return err
			}
			if pk > 0 {
				got = append(got, kc{n, pk})
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading primary key of %q: %v", table, err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].pos < got[j].pos })
	out := make([]string, 0, len(got))
	for _, g := range got {
		out = append(out, g.name)
	}
	return out
}

// w8NotNullColumns returns the set of NOT NULL columns of a table.
func w8NotNullColumns(t *testing.T, st *store.Store, table string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name, "notnull" FROM pragma_table_info(?)`, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			var nn int
			if err := rows.Scan(&n, &nn); err != nil {
				return err
			}
			if nn != 0 {
				out[n] = true
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading notnull of %q: %v", table, err)
	}
	return out
}

func w8SetEqual(got, want []string) (missing, extra []string) {
	g := map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	w := map[string]bool{}
	for _, s := range want {
		w[s] = true
	}
	for _, s := range want {
		if !g[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range got {
		if !w[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return
}

// w8HasColumn reports whether any column of table matches pred.
func w8HasColumn(cols []string, pred func(string) bool) (string, bool) {
	for _, c := range cols {
		if pred(c) {
			return c, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// W8-2 — the four prohibitions, asserted ABSENT of the BUILT shape
// ---------------------------------------------------------------------------

// TestW8TradeShapesCarryNoneOfTheFourProhibitions is W8-2.
//
//	(a) no company_id, or any column scoping a trade to a company however
//	    spelled                                        ⟦DR-W8-2⟧
//	(b) no kind-shaped discriminator on `trade`        ⟦DR-W8-3⟧
//	(c) no membership column and no membership table   D-23, verbatim:
//	    "Crew membership is NOT modelled. Do not add it back under another
//	    name."
//	(d) no `retired` on trade OR as a fact on trade_amendment   D-30(b)
//	(e) no reason/note/comment/motive on trade_amendment        D-49 bc 1
//	(f) no recorded_by on trade_amendment — while trade_created HAS it
//	                                                   ⟦DR-W8-5⟧
//
// ⚠ THE ASSERTION IS SCOPED TO THE TRADE SHAPES, NEVER TO THE WORD, and that
// is MEASURED rather than cautionary: `person_amended` carries a LEGITIMATE
// `retired` fact and column (D-21.5's person retirement), so "a bare
// grep -i retired reds against a locked, shipped, correct feature".
//
// ⭐ PRECONDITION, failing loud in w5d_scoped_read_test.go:216-228's landed
// idiom ⟦LOCK — FAL-F4⟧: the `trade` table exists and its column set is
// non-empty. ⚠ WITHOUT IT ALL SIX LIMBS ARE RED PRE-CHUNK FOR THE RIGHT
// REASON AND GREEN POST-CHUNK FOR A WRONG ONE — a dropped or mis-named
// `trade` table would make five of the six absence assertions pass BY THE
// TABLE DISAPPEARING, which is a green that means nothing.
// ⚠ `PRECONDITION:` is not a production red and must not be reported as one.
//
// ⭐ GENUINE PRE-CHUNK RED in the introspection form — the precondition fires
// naming no unbuilt Go symbol.
func TestW8TradeShapesCarryNoneOfTheFourProhibitions(t *testing.T) {
	st := newDomainTestStore(t)

	tradeCols := w8Columns(t, st, "trade")
	if len(tradeCols) == 0 {
		t.Fatalf("PRECONDITION: no such table: trade — pragma_table_info(\"trade\") returned no columns. "+
			"⚠ THIS IS THE PRE-CHUNK RED W8-2 IS ANCHORED BY, and it is stated as a PRECONDITION rather than "+
			"as six passing absence assertions for the reason ⟦LOCK — FAL-F4⟧ gives: without it all six limbs "+
			"are red pre-chunk for the RIGHT reason and green post-chunk for a WRONG one — a dropped or "+
			"mis-named `trade` table makes five of the six pass BY THE TABLE DISAPPEARING.\n"+
			"sqlite_master reports: %v", w8TableNames(t, st))
	}
	amendCols := w8Columns(t, st, "trade_amendment")
	if len(amendCols) == 0 {
		t.Fatalf("PRECONDITION: no such table: trade_amendment — limbs (d), (e) and (f) have no subject")
	}

	// (a) no company_id, however spelled — a column that SCOPES a trade to a
	//     company. The filter is on the reference SHAPE, not one spelling.
	if c, ok := w8HasColumn(tradeCols, func(c string) bool {
		l := strings.ToLower(c)
		return l == "company_id" || (strings.Contains(l, "company") && strings.HasSuffix(l, "_id"))
	}); ok {
		t.Errorf("W8-2(a) ((m8-1)): `trade` carries %q — a trade is NOT scoped to a company ⟦DR-W8-2⟧. "+
			"This is the workgroup shape W8 deletes, reappearing on its successor", c)
	}

	// (b) no kind-shaped discriminator — a column partitioning trade rows
	//     into classes. The dead `workgroup.kind` construction is the named
	//     precedent ⟦DR-W8-3⟧.
	if c, ok := w8HasColumn(tradeCols, func(c string) bool {
		l := strings.ToLower(c)
		return l == "kind" || l == "type" || l == "class" || l == "category"
	}); ok {
		t.Errorf("W8-2(b) ((m8-2)): `trade` carries %q — a kind-shaped discriminator partitioning trade rows "+
			"into classes ⟦DR-W8-3⟧", c)
	}

	// (c)(i) the EXACT column set — this is what catches trade(lead_person_id)
	//        and every other membership column, and it is asserted as a SET,
	//        never as a size.
	if missing, extra := w8SetEqual(tradeCols, w8TradeColumns); len(missing) != 0 || len(extra) != 0 {
		t.Errorf("W8-2(c)(i) / W8-X4 part (a): `trade`'s column set is wrong — missing %v, unexpected %v. "+
			"DR-W8-4 pins EXACTLY seven: %v. ⚠ A membership column of ANY name (lead_person_id, holder, "+
			"roll, roles) is caught HERE, which is why limb (c) is three sub-limbs and not a spelling filter. "+
			"got: %v", missing, extra, w8TradeColumns, tradeCols)
	}

	// (c)(ii) THE PK-DERIVED PROPERTY, over sqlite_master's table set.
	w8AssertNoMembershipRowType(t, st)

	// (c)(iii) no trade_created/trade_amended payload key references a person.
	//          Asserted over the AMENDMENT TABLE's columns (the wire keys'
	//          landing place) — the payload half is asserted on the wire at
	//          W8-23(a)'s instrument.
	if c, ok := w8HasColumn(amendCols, func(c string) bool {
		l := strings.ToLower(c)
		return strings.Contains(l, "person")
	}); ok {
		t.Errorf("W8-2(c)(iii): `trade_amendment` carries %q — a trade may not know its people. "+
			"D-23, verbatim: \"Crew membership is NOT modelled. Do not add it back under another name.\" "+
			"⚠ What limb (c) forbids is the REVERSE direction: person.trade_id is the ruled model", c)
	}

	// (d) no `retired`, on either shape.
	for _, tc := range []struct {
		table string
		cols  []string
	}{{"trade", tradeCols}, {"trade_amendment", amendCols}} {
		if c, ok := w8HasColumn(tc.cols, func(c string) bool { return strings.EqualFold(c, "retired") }); ok {
			t.Errorf("W8-2(d) ((m8-6)): %s carries %q. D-30(b) rules there is no retirement for trade. "+
				"⚠ Scoped to the TRADE shapes and never to the word: person_amended carries a LEGITIMATE "+
				"`retired` fact (D-21.5), so a bare `grep -i retired` reds against a shipped, correct feature",
				tc.table, c)
		}
	}

	// (e) no reason/note/comment/motive on trade_amendment — D-49 binding
	//     consequence 1, which names "the vehicle/trade carriers W7/W8 clone".
	if c, ok := w8HasColumn(amendCols, func(c string) bool {
		l := strings.ToLower(c)
		return l == "reason" || l == "note" || l == "comment" || strings.Contains(l, "motive") || strings.Contains(l, "_note")
	}); ok {
		t.Errorf("W8-2(e) ((m8-7)): `trade_amendment` carries %q — a free-text motive column. "+
			"D-49 binding consequence 1 names W8's carrier in terms: \"the vehicle/trade carriers W7/W8 clone\"", c)
	}

	// (f) no recorded_by on trade_amendment — while trade_created HAS it.
	// ⚠ ⟦LOCK — CIT-F4⟧ THIS LIMB ENCODES THE UNRULED BRANCH OF §10(a) Q6. It
	// asserts ABSENCE because the clone forces the EXACT-SET form (D-49
	// binding consequence 3) — NOT because attribution has been ruled
	// against. A *yes* to Q6 makes this limb and W8-X4's trade_amendment row
	// the two that change. ⚠ Do NOT drop the limb.
	if c, ok := w8HasColumn(amendCols, func(c string) bool { return strings.EqualFold(c, "recorded_by") }); ok {
		t.Errorf("W8-2(f) ((m8-8)): `trade_amendment` carries %q ⟦DR-W8-5⟧. Neither person_amendment nor "+
			"company_amendment carries one, while trade_created DOES — the two trade shapes deliberately "+
			"differ. ⚠ This absence encodes the UNRULED branch of §10(a) Q6 (nothing records who changed a "+
			"positional handset number); it is not a ruling against attribution", c)
	}

	// The exact ten, as a SET.
	if missing, extra := w8SetEqual(amendCols, w8TradeAmendmentColumns); len(missing) != 0 || len(extra) != 0 {
		t.Errorf("W8-X4 part (a) / W8-15: `trade_amendment`'s column set is wrong — missing %v, unexpected %v. "+
			"DR-W8-4 pins EXACTLY ten: %v. ⚠ Supersession is computed AT READ, so NO STATUS-SHAPED COLUMN may "+
			"ever join this table. got: %v", missing, extra, w8TradeAmendmentColumns, amendCols)
	}
}

// w8AssertNoMembershipRowType is W8-2(c)(ii) — THE PK-DERIVED PROPERTY.
//
// ⟦LOCK — B-21: the "identity is the pair" carve-out is SUPERSEDED, because
// it WAS the hole. Stated that way it admits
// trade_member(id PRIMARY KEY, trade_id, person_id) — a surrogate key makes
// the identity a single column while the row type is still a membership row.⟧
//
// THE PROPERTY: a table carrying BOTH a trade-shaped and a person-shaped
// reference must NOT have that pair AS, or IN, its PRIMARY KEY, and NEITHER
// reference may be NOT NULL alongside the other.
//
// ⚠ THE REFERENCE SHAPES ARE MATCHED, NOT THE COLUMN NAMES, and that is what
// FIX-F12's plant `trade_roll(trade, person_ref, PRIMARY KEY(trade, person_ref))`
// exists to prove: it carries NEITHER the string `trade_id` NOR the string
// `person_id`, "so an implementation that matched column NAMES rather than
// REFERENCE SHAPES passes it and the property does not".
//
// FOUR DISTINGUISHABLE OUTCOMES ON FOUR INPUTS — which is what makes it a
// property and not a list:
//
//	person                                    PASS — keyed on person_id; trade_id is a nullable tag
//	visit                                     PASS — keyed on visit_id; the trade reference is a nullable SNAPSHOT
//	trade_member(id PK, trade_id, person_id)  FLAG — the surrogate-keyed spelling
//	trade(lead_person_id)                     caught by (c)(i), not by this limb
func w8AssertNoMembershipRowType(t *testing.T, st *store.Store) {
	t.Helper()
	for _, table := range w8TableNames(t, st) {
		cols := w8Columns(t, st, table)
		tradeRef, hasTrade := w8HasColumn(cols, w8IsTradeRef)
		personRef, hasPerson := w8HasColumn(cols, w8IsPersonRef)
		if !hasTrade || !hasPerson {
			continue
		}
		pk := w8PrimaryKeyColumns(t, st, table)
		inPK := map[string]bool{}
		for _, c := range pk {
			inPK[c] = true
		}
		if inPK[tradeRef] && inPK[personRef] {
			t.Errorf("W8-2(c)(ii) ((m8-2)): table %q carries a trade-shaped reference (%q) and a person-shaped "+
				"reference (%q) and BOTH are in its PRIMARY KEY %v. That is a MEMBERSHIP ROW TYPE. "+
				"D-23, verbatim: \"Crew membership is NOT modelled. Do not add it back under another name.\"",
				table, tradeRef, personRef, pk)
			continue
		}
		nn := w8NotNullColumns(t, st, table)
		if nn[tradeRef] && nn[personRef] {
			t.Errorf("W8-2(c)(ii) ((m8-2)): table %q carries a trade-shaped reference (%q) and a person-shaped "+
				"reference (%q) and NEITHER is nullable alongside the other. That is a membership row type "+
				"wearing a surrogate key — the spelling the superseded \"identity is the pair\" carve-out let "+
				"through. Primary key: %v", table, tradeRef, personRef, pk)
		}
	}
}

// w8IsTradeRef / w8IsPersonRef match a REFERENCE SHAPE, not a column name:
// a column whose name is, or ends in, the entity token, optionally suffixed
// by an id/ref marker. This is what FIX-F12's deliberately-misnamed
// `trade_roll(trade, person_ref, …)` plant is for.
func w8IsTradeRef(c string) bool  { return w8IsEntityRef(c, "trade") }
func w8IsPersonRef(c string) bool { return w8IsEntityRef(c, "person") }

func w8IsEntityRef(col, entity string) bool {
	l := strings.ToLower(col)
	if l == entity {
		return true
	}
	for _, suffix := range []string{"_id", "_ref", "_key", "_fk"} {
		if l == entity+suffix {
			return true
		}
	}
	// A snapshot is deliberately NOT a reference for this property's
	// purposes: `visit.trade_snapshot_id` records WHICH trade was used and is
	// nullable by design (W8-8(b)). It is separated by the nullability half
	// anyway, but naming it here keeps the two reasons apart.
	return false
}

// TestW8MembershipPropertyHasItsTwoNamedPlants produces W8-2(c)(ii)'s
// positive controls. ⚠ TWO PLANTS, and the second is named so no spelling
// filter sees it:
//
//	(A) trade_member(id TEXT PRIMARY KEY, trade_id TEXT NOT NULL,
//	    person_id TEXT NOT NULL)      — FAL-F5's surrogate case
//	(B) trade_roll(trade, person_ref, PRIMARY KEY(trade, person_ref))
//	                                  — FIX-F12's deliberately-misnamed plant,
//	    which carries NEITHER the string `trade_id` NOR `person_id`
//
// and the two NEGATIVE rows — `person` and `visit` — which the superseded
// form of this limb RED ON. ⚠ FAL-F5's proposed one-name exemption
// ("except `person`, named") is REJECTED on measurement: `visit` carries both
// references too and is not exempted by it.
func TestW8MembershipPropertyHasItsTwoNamedPlants(t *testing.T) {
	st := newDomainTestStore(t)

	// The NEGATIVE rows first: the property must PASS on the correct build's
	// own tables, or it is red on the correct tree and unimplementable.
	sub := &w8Recorder{}
	w8AssertNoMembershipRowTypeInto(t, st, sub)
	if len(sub.msgs) != 0 {
		t.Fatalf("W8-2(c)(ii) CONTROL FAILED (negative rows): the PK-derived property flags a table on the "+
			"CORRECT tree: %v.\n⚠ Revision 2's limb was unimplementable in exactly this way — it reds on "+
			"`person` (person_id + trade_id) and on `visit` (person_id NOT NULL + the trade snapshot). "+
			"A property that is red on the correct build cannot judge a mutant", sub.msgs)
	}

	// PLANT (A): the surrogate-keyed spelling.
	w8Exec(t, st, `CREATE TABLE trade_member (id TEXT PRIMARY KEY, trade_id TEXT NOT NULL, person_id TEXT NOT NULL) STRICT`)
	plantA := &w8Recorder{}
	w8AssertNoMembershipRowTypeInto(t, st, plantA)
	if len(plantA.msgs) == 0 {
		t.Errorf("W8-2(c)(ii) CONTROL FAILED, PLANT (A): trade_member(id PRIMARY KEY, trade_id NOT NULL, " +
			"person_id NOT NULL) was NOT flagged. This is the surrogate-keyed spelling the superseded " +
			"\"identity is the pair\" carve-out let through — and it is the natural spelling of (m8-2)")
	}
	w8Exec(t, st, `DROP TABLE trade_member`)

	// PLANT (B): the deliberately-misnamed one. NEITHER the string `trade_id`
	// NOR `person_id` appears in it.
	w8Exec(t, st, `CREATE TABLE trade_roll (trade TEXT NOT NULL, person_ref TEXT NOT NULL, PRIMARY KEY (trade, person_ref)) STRICT`)
	plantB := &w8Recorder{}
	w8AssertNoMembershipRowTypeInto(t, st, plantB)
	if len(plantB.msgs) == 0 {
		t.Errorf("W8-2(c)(ii) CONTROL FAILED, PLANT (B): trade_roll(trade, person_ref, PRIMARY KEY(trade, person_ref)) " +
			"was NOT flagged. It carries NEITHER the string `trade_id` NOR `person_id`, so an implementation " +
			"matching column NAMES rather than REFERENCE SHAPES passes it — which is the whole reason this " +
			"plant is named in the criterion")
	}
	w8Exec(t, st, `DROP TABLE trade_roll`)
}

// w8Recorder collects what the property would have reported, so the plants
// above can be produced without failing the test that produces them.
type w8Recorder struct{ msgs []string }

func (r *w8Recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

type w8Reporter interface {
	Errorf(format string, args ...any)
}

// w8AssertNoMembershipRowTypeInto is w8AssertNoMembershipRowType with its
// reporter injected, so the same code judges the real tree and the plants.
// ⚠ The plants are driven THROUGH the property, never past it.
func w8AssertNoMembershipRowTypeInto(t *testing.T, st *store.Store, rep w8Reporter) {
	t.Helper()
	for _, table := range w8TableNames(t, st) {
		cols := w8Columns(t, st, table)
		tradeRef, hasTrade := w8HasColumn(cols, w8IsTradeRef)
		personRef, hasPerson := w8HasColumn(cols, w8IsPersonRef)
		if !hasTrade || !hasPerson {
			continue
		}
		pk := w8PrimaryKeyColumns(t, st, table)
		inPK := map[string]bool{}
		for _, c := range pk {
			inPK[c] = true
		}
		if inPK[tradeRef] && inPK[personRef] {
			rep.Errorf("table %q: trade-shaped %q and person-shaped %q are both IN the primary key %v", table, tradeRef, personRef, pk)
			continue
		}
		nn := w8NotNullColumns(t, st, table)
		if nn[tradeRef] && nn[personRef] {
			rep.Errorf("table %q: trade-shaped %q and person-shaped %q are both NOT NULL", table, tradeRef, personRef)
		}
	}
}

func w8Exec(t *testing.T, st *store.Store, sqlText string) {
	t.Helper()
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(sqlText)
		return err
	}); err != nil {
		t.Fatalf("fixture defect: %s: %v", sqlText, err)
	}
}

// ---------------------------------------------------------------------------
// W8-X4 / W8-23(a) — the four tables, and the person set that swaps net-zero
// ---------------------------------------------------------------------------

// TestW8PersonColumnSetLosesWorkgroupAndGainsTrade is W8-X4's `person` row
// and W8-23(a)'s SECOND instrument.
//
// ⟦LOCK — ENU-F12: this row is a RUNTIME PRE-CHUNK RED — the same instrument
// as W8-23(a) — and it fell through all three of §6's accounts.⟧
//
// ⚠⚠ THE EXACT SET, NEVER ITS SIZE. [MEASURED §2.9] `person` loses
// `workgroup_id` and gains `trade_id`: ELEVEN BEFORE, ELEVEN AFTER. "A test
// asserting a count passes through the change unchanged."
//
// ⭐ PRODUCED PRE-CHUNK RED at runtime, naming no unbuilt Go symbol.
func TestW8PersonColumnSetLosesWorkgroupAndGainsTrade(t *testing.T) {
	st := newDomainTestStore(t)
	cols := w8Columns(t, st, "person")
	if len(cols) == 0 {
		t.Fatalf("PRECONDITION: no such table: person — the instrument, not W8")
	}
	missing, extra := w8SetEqual(cols, w8PersonColumnsAfterW8)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("W8-X4 (`person` row) / W8-23(a): the `person` column SET is wrong — missing %v, unexpected %v.\n"+
			"want (exact set): %v\ngot:              %v\n"+
			"⚠ ASSERTED AS A SET AND NEVER AS A SIZE: person LOSES workgroup_id and GAINS trade_id, so it is "+
			"ELEVEN COLUMNS BEFORE AND ELEVEN AFTER — a count-shaped assertion passes straight through the change, "+
			"which is the same arithmetic that hides the registry's net-zero event-type swap.\n"+
			"⚠ D-66 puts a person's trade in the CREATION record; the addition is what makes W5a-G-2's "+
			"post-amendment reading (permitted union = pre-chunk set PLUS all declared additions) load-bearing here",
			missing, extra, w8PersonColumnsAfterW8, cols)
	}
}

// TestW8VisitSnapshotColumnsAreRenamedToTrade is W8-X4's `visit` row.
//
// ⚠ THIS COLLIDES WITH W6-7(a)'s LOCKED EXACT-SET PIN and is routed as
// SEAM-W8-4 — ten per-site lock amendments the PLAN OWNER owns, triggered
// BEFORE W8's builder. ⚠ W8-A6 measured that SIX of the ten do NOT produce
// the red the seam claimed (4 RED, 5 DARK, 1 GREEN), so an amender expecting
// ten reds will see four and stop. This test is one of the four that REDS.
func TestW8VisitSnapshotColumnsAreRenamedToTrade(t *testing.T) {
	st := newDomainTestStore(t)
	cols := w8Columns(t, st, "visit")
	if len(cols) == 0 {
		t.Fatalf("PRECONDITION: no such table: visit — the instrument, not W8")
	}
	missing, extra := w8SetEqual(cols, w8VisitColumnsAfterW8)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("W8-X4 (`visit` row): the `visit` column SET is wrong — missing %v, unexpected %v.\n"+
			"want (exact set): %v\ngot:              %v\n"+
			"⚠ W8 RENAMES two live columns, workgroup_snapshot_{id,name} -> trade_snapshot_{id,name}. "+
			"SQLite has no ALTER COLUMN, so the rename is a table rebuild or a fresh CREATE — which is why "+
			"W8-X1's forbidden set had to gain DROP TABLE / RENAME TO.\n"+
			"⚠ This assertion COLLIDES with W6-7(a)'s LOCKED exact-set pin and the collision is SEAM-W8-4, "+
			"the plan owner's, triggered before W8's builder — not a builder's to resolve",
			missing, extra, w8VisitColumnsAfterW8, cols)
	}
}

// TestW8PersonCreatedCarriesATradeIDWireKey is W8-23(a)'s FIRST instrument —
// the WIRE half, asserted on the MARSHALLED BYTES of a real authored
// `person_created`.
//
// The criterion: "Asserted TWICE, on different media, because they freeze
// differently: the MARSHALLED BYTES of a real authored person_created carry
// the literal key `trade_id` (⚠ the wire half — B-2 row 15), AND the
// introspected `person` column set contains trade_id and no longer contains
// workgroup_id."
//
// ⚠ B-2's row for this literal is flagged as one of TWO PAIRS to read twice:
// `trade_id` as a PersonCreated payload key and `workgroup_id` as one are
// A KEY INSIDE HASHED BYTES — one deleted, one added, same chunk.
//
// ⭐ PRODUCED PRE-CHUNK RED at runtime: a marshalled person_created carries
// no trade_id key today.
func TestW8PersonCreatedCarriesATradeIDWireKey(t *testing.T) {
	st := newDomainTestStore(t)

	co, err := CreateCompany(st, "site-a", 1, "Acme Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// The person is created through the ORDINARY exported authoring path —
	// this is a wire-shape assertion about what that path EMITS, not about a
	// hand-built envelope.
	if _, err := CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	var created []map[string]any
	for _, e := range w3Events(t, st, "site-a", 1) {
		var m map[string]any
		if err := json.Unmarshal(e.Payload, &m); err != nil {
			t.Fatalf("fixture defect: stored payload is not an object: %s", e.Payload)
		}
		if m["type"] == "person_created" {
			created = append(created, m)
		}
	}
	if len(created) != 1 {
		t.Fatalf("PRECONDITION: the fixture authored %d person_created events, want exactly 1 — "+
			"this assertion is about the bytes ONE authoring call emits", len(created))
	}
	p := created[0]

	if _, ok := p["trade_id"]; !ok {
		keys := make([]string, 0, len(p))
		for k := range p {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("W8-23(a) (the WIRE half): the marshalled bytes of an authored `person_created` carry no "+
			"`trade_id` key. Keys present: %v.\n"+
			"⚠ D-66 puts a person's trade in the CREATION record — \"how most persons enter the roster\" — "+
			"and B-2 freezes `trade_id` as a PersonCreated payload key, which is A KEY INSIDE HASHED BYTES. "+
			"⚠ The column-set half (TestW8PersonColumnSetLosesWorkgroupAndGainsTrade) is a DIFFERENT medium "+
			"and does not substitute: the two freeze differently", keys)
	}
	if _, ok := p["workgroup_id"]; ok {
		t.Errorf("W8-23(a) / W8-6(b): the marshalled bytes of an authored `person_created` still carry a " +
			"`workgroup_id` key. B-2's REMOVE list carries it as a PersonCreated payload key — ALSO INSIDE " +
			"HASHED BYTES — and the first-issue text left the wire-key deletion implied")
	}
}
