// w6_scoped_read_test.go — W6-1(c): the populate adds NO store read.
//
// ⚠ THIS FILE DISCHARGES SEAM-13. The lock states, in terms, that
// W6-1(c)'s instrument rests on an [INFERENCE] nobody had measured: that a
// TEMP VIEW can shadow `person` and `person_amendment` on the store's single
// connection the way w5d_scoped_read_test.go shadows `company_amendment`.
// The probe-COLUMN analysis was measured (`cleared`; `recorded_by`); the
// SHADOW was not. It is measured here — CONTROL 1 and CONTROL 2 below are
// that measurement, and they fail saying the INSTRUMENT is broken rather
// than letting a mis-calibrated probe red a production assertion.
//
// ⚠ NEVER A STOPWATCH (plan:6577-6579, and this environment's own standing
// rule: internal/domain measures 205-572s under concurrent load). The number
// asserted is rows DECODED, which is a property of the SQL the read issues
// and is identical on a fast machine and a loaded one.
//
// ⚠ THE COUNT IS A LITERAL, NEVER A DELTA. The lock is explicit: "a delta of
// zero is what both the correct build and a build that reads twice-then-
// caches produce."
//
// ⚠ BOTH tables are shadowed, and that is not belt-and-braces: PersonByID
// (refdata.go:668-685) reads `person` via oneRow and then COMPOSES over
// readPersonAmendmentsFor. Shadowing one leaves the other's decode
// invisible — and a writer who shadowed only `visit`-adjacent tables would
// get a clean pass under (m6-3), whose planted extra read is of `person`.
//
// ⚠ THIS HALF IS AT RISK OF BEING SELF-CONFIRMING and says so: the natural
// implementation (`v.PersonName = &p.Name` inside the existing loop) CANNOT
// fail it, because `p` is already in hand at tripauthor.go:404. Its evidence
// is (m6-3) — a planted PersonByID/Persons() call at the fold — never the
// clean run.
package domain

import (
	"database/sql"
	"database/sql/driver"
	"sync"
	"sync/atomic"
	"testing"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/store"
)

var (
	w6PersonRowsDecoded    atomic.Int64
	w6AmendmentRowsDecoded atomic.Int64
	w6ProbeOnce            sync.Once
)

const (
	w6PersonProbeFn    = "w6_person_rows_decoded_probe"
	w6AmendmentProbeFn = "w6_person_amendment_rows_decoded_probe"
)

// w6RegisterRowProbes registers both counting scalar functions. modernc
// exposes new functions to connections opened AFTER registration, so this
// runs before the store is opened.
func w6RegisterRowProbes(t *testing.T) {
	t.Helper()
	w6ProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(w6PersonProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w6PersonRowsDecoded.Add(1)
				return args[0], nil
			})
		msqlite.MustRegisterScalarFunction(w6AmendmentProbeFn, 1,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				w6AmendmentRowsDecoded.Add(1)
				return args[0], nil
			})
	})
}

// w6InstallRowProbeViews shadows BOTH tables with views of the same shape.
//
// The probe columns are the ones the lock names, and the choice is
// load-bearing in exactly w5d's way: the probe must wrap a column appearing
// in NEITHER the WHERE nor the ORDER BY, or it fires once per row SCANNED or
// once per SORT COMPARISON instead of once per row EMITTED.
//
//	person             `recorded_by` — the oneRow read filters on person_id
//	                   and the projection's OrderBy is person_id; recorded_by
//	                   is in neither.
//	person_amendment   `cleared` — the composition selects sixteen columns,
//	                   filters on person_id and orders on
//	                   site_id, node_epoch, seq; cleared is in none of them.
//
// ⚠ Installed only AFTER the fixture is complete: a view cannot be INSERTed
// into, so every write in this test happens first and nothing writes after.
func w6InstallRowProbeViews(t *testing.T, st *store.Store) {
	t.Helper()
	err := st.LocalTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TEMP VIEW person AS
			SELECT person_id, site_id, name, company_id, workgroup_id,
			       ` + w6PersonProbeFn + `(recorded_by) AS recorded_by,
			       last_name, first_name, position, phone
			FROM main.person`); err != nil {
			return err
		}
		_, err := tx.Exec(`CREATE TEMP VIEW person_amendment AS
			SELECT site_id, node_epoch, seq, person_id, name, last_name,
			       first_name, phone, company_id, trade_id, role,
			       position, retired, same_as,
			       ` + w6AmendmentProbeFn + `(cleared) AS cleared, seen
			FROM main.person_amendment`)
		return err
	})
	if err != nil {
		t.Fatalf("CONTROL FAILED (instrument): could not install the rows-decoded shadow views over "+
			"`person` and `person_amendment`: %v.\n"+
			"  ⚠ THIS IS SEAM-13's MEASUREMENT. If a TEMP VIEW cannot shadow these two tables, W6-1(c) needs "+
			"a DIFFERENT instrument — and that is a FINDING, not a licence to relax the assertion. "+
			"A stopwatch is not the fallback (plan:6577-6579); store-level introspection of the issued SQL is.", err)
	}
}

// w6Measure runs fn and returns (person rows decoded, person_amendment rows
// decoded) while it ran. Readings are DELTAS around one call so nothing
// leaks between measurements; the ASSERTIONS below are literals.
func w6Measure(fn func()) (int64, int64) {
	p0, a0 := w6PersonRowsDecoded.Load(), w6AmendmentRowsDecoded.Load()
	fn()
	return w6PersonRowsDecoded.Load() - p0, w6AmendmentRowsDecoded.Load() - a0
}

// w6CountRows counts rows in the REAL tables (main-qualified, so it never
// goes through a view and never disturbs the counters).
func w6CountRows(t *testing.T, st *store.Store, table, where string, args ...any) int {
	t.Helper()
	var n int
	err := st.LocalTx(func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM main.`+table+` `+where, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting %s rows: %v", table, err)
	}
	return n
}

// TestW6ArrivalAuthoringDecodesNoExtraPersonRows — W6-1(c), as ROWS DECODED
// per check-in.
//
// EXPECTED RED at f33c4c7: NONE. The populate does not exist, so it adds no
// read, and this criterion is ALREADY TRUE (plan §2.5 row 2) — the loop
// already holds the whole composed person at tripauthor.go:404, so `p.Name`
// costs nothing. ⚠ Its clean pass is NOT evidence. Its evidence is (m6-3):
// a `PersonByID`/`Persons()` call planted at the fold or in the read path,
// which raises the person count above the pinned literal and reds here.
func TestW6ArrivalAuthoringDecodesNoExtraPersonRows(t *testing.T) {
	w6RegisterRowProbes(t)

	st := w6NewStore(t)
	target, err := CreatePerson(st, "site-a", 1, "Vega, Ana", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	decoy, err := CreatePerson(st, "site-a", 1, "Decoy, Dee", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	const targetAmendments, decoyAmendments = 3, 9
	for i := 0; i < targetAmendments; i++ {
		w6AmendName(t, st, "site-a", 1, target.PersonID, "Vega, Ana rev")
	}
	for i := 0; i < decoyAmendments; i++ {
		w6AmendName(t, st, "site-a", 1, decoy.PersonID, "Decoy, Dee rev")
	}

	// --- the fixture asserts its own precondition (rule 6) --------------
	// A rows-decoded assertion defends nothing unless a scoped read and a
	// whole-table read score DIFFERENTLY: the target must own SOME rows, and
	// the table must hold MORE than the target's.
	totalPersons := w6CountRows(t, st, "person", "")
	totalAmendments := w6CountRows(t, st, "person_amendment", "")
	targetAmendmentRows := w6CountRows(t, st, "person_amendment", "WHERE person_id = ?", target.PersonID)
	if totalPersons != 2 || targetAmendmentRows != targetAmendments || totalAmendments != targetAmendments+decoyAmendments {
		t.Fatalf("PRECONDITION: the fixture did not land as written — person holds %d rows, person_amendment %d "+
			"(%d the target's), want 2, %d, %d",
			totalPersons, totalAmendments, targetAmendmentRows, targetAmendments+decoyAmendments, targetAmendments)
	}
	if totalAmendments <= targetAmendmentRows || totalPersons <= 1 {
		t.Fatalf("PRECONDITION: the fixture poses no case — a whole-table read and a scoped read would decode "+
			"the same number and this test could never fail (persons=%d, amendments=%d, target's=%d)",
			totalPersons, totalAmendments, targetAmendmentRows)
	}

	w6InstallRowProbeViews(t, st)

	// --- CONTROL 1: the instrument is live, and reads HIGH correctly -----
	var allPersons []Person
	hiP, hiA := w6Measure(func() { allPersons, err = Persons(st) })
	if err != nil {
		t.Fatal(err)
	}
	if len(allPersons) != totalPersons || hiP != int64(totalPersons) || hiA != int64(totalAmendments) {
		t.Fatalf("CONTROL FAILED (instrument, high end): the whole-roster composed read returned %d persons and "+
			"the probes counted person=%d person_amendment=%d, want %d / %d / %d.\n"+
			"  Either a TEMP VIEW is not shadowing these tables or a probe is not firing once per emitted row — "+
			"this is the INSTRUMENT, not the populate. ⚠ SEAM-13: if this cannot be made to hold, W6-1(c) needs "+
			"a different instrument and that is a FINDING, not a licence to relax the assertion.",
			len(allPersons), hiP, hiA, totalPersons, totalPersons, totalAmendments)
	}

	// --- CONTROL 2: the instrument reads LOW correctly -------------------
	// If SQLite stopped flattening the views the probes would fire per
	// SCANNED row and report the whole table — which is what a mis-calibrated
	// instrument would report for the authoring path too, indistinguishable
	// from a real regression.
	loP, loA := w6Measure(func() { _, err = PersonByID(st, decoy.PersonID) })
	if err != nil {
		t.Fatal(err)
	}
	if loP != 1 || loA != int64(decoyAmendments) {
		t.Fatalf("CONTROL FAILED (instrument, low end): one scoped PersonByID decoded person=%d "+
			"person_amendment=%d, want 1 / %d. The probes cannot tell a scoped read from a whole-table one, "+
			"so they cannot judge the populate.", loP, loA, decoyAmendments)
	}

	// --- the real judgement: pinned LITERALS, never a delta --------------
	// One occupant, one roster row, that occupant's own amendment rows.
	const wantPersonRows, wantAmendmentRows = int64(1), int64(targetAmendments)
	gotP, gotA := w6Measure(func() {
		w6CheckInUnsolicited(t, st, "site-a", 1, []string{target.PersonID})
	})
	if gotP != wantPersonRows || gotA != wantAmendmentRows {
		t.Errorf("W6-1(c) (m6-3): authoring ONE arrival decoded person=%d person_amendment=%d rows, "+
			"want the pinned literals person=%d person_amendment=%d.\n"+
			"  The populate must add NO store read: buildArrivalVisits already holds the whole composed person "+
			"(tripauthor.go:404), so `p.Name` costs nothing. A count of person=%d is exactly one extra "+
			"PersonByID per occupant.\n"+
			"  ⚠ The number is pinned as a LITERAL and not as a delta, deliberately: a delta of zero is what "+
			"both the correct build and a build that reads twice-then-caches produce.",
			gotP, gotA, wantPersonRows, wantAmendmentRows, 2*wantPersonRows)
	}

	// The count is only worth having if the read it counts is the read that
	// ANSWERS: a populate that read nothing would score a perfect 1/3.
	//
	// ⟦LOCK — H-6, post-lock amendment 2026-09-02. PROMOTED from t.Logf to a
	// FAILURE, on a discriminator that keeps W6-1(c) ⟦NO PRE-CHUNK RED⟧
	// (plan §2.5 row 2).
	// ⟦was: a bare `if _, present := w6SnapshotOf(t, v); !present { t.Logf(…)
	// }` — a soft hint that could not fail in either epoch.⟧⟧
	//
	// THE DISCRIMINATOR, and it is not circular: `w6SnapshotOf` reports
	// (value, present) and returns present=false for BOTH "no such field" and
	// "field present, JSON null". The MARSHALLED KEY SET separates them.
	// Visit.PersonSnapshotName carries no `omitempty` (trip.go:280), so
	//
	//	key ABSENT   -> the struct has no such field: f33c4c7, the pre-chunk
	//	                epoch. Nothing to judge, and reddening here would
	//	                contradict the lock's already-true row. LOGGED.
	//	key PRESENT  -> the field exists, so the build landed. A NULL or an
	//	                EMPTY value now means the populate did not populate,
	//	                and this criterion's clean count of 1/3 is counting a
	//	                read that answers nothing. FAILS.
	//
	// Observable: null out the populate at tripauthor.go and this reds while
	// the rows-decoded literals above stay green — which is precisely the
	// self-confirming reading H-6 was raised against.
	v := w6VisitFor(t, st, "site-a", target.PersonID)
	snap, present := w6SnapshotOf(t, v)
	if !w6Has(w6VisitKeys(t, v), w6VisitSnapshotKey) {
		t.Logf("W6-1(c) NOTE: the marshalled visit carries no %s KEY AT ALL, so the projection struct has no "+
			"such field: this is the f33c4c7 pre-chunk state, and W6-1(b) is the criterion that reds for it.",
			w6VisitSnapshotKey)
	} else if !present || snap == "" {
		t.Errorf("W6-1(c): the arrival landed with the %s key PRESENT but %s. The field exists, so the build is "+
			"in — and this criterion's pinned count of person=1 person_amendment=3 is then counting a populate "+
			"that does not populate, which is the self-confirming reading the criterion marks itself at risk of.\n"+
			"  ⚠ This is NOT the pre-chunk state: at f33c4c7 the key is absent from the marshalled object "+
			"entirely (the struct field does not exist), and that arm is logged, not failed.",
			w6VisitSnapshotKey, map[bool]string{true: "EMPTY", false: "NULL"}[present])
	}
}

// w6Has reports whether xs contains s.
func w6Has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
