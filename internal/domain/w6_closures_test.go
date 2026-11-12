// w6_closures_test.go — W6-4, W6-5 and W6-8's behavioural half: the two
// resolve closures, asserted at the VALUE level.
//
// DR-W6-5 (carrying reports/SCHEMA10-ARBITER-VERDICT.md adjudication #7,
// "RULED … Adjudication closed") gives the two boards two different
// preference orders, and that asymmetry is the whole of W6's read:
//
//	MusterReprint (every printed sheet)  snapshot -> live Name -> raw PersonID
//	MusterLive    (/muster)              live Name -> raw PersonID   (NO snapshot arm)
//
// DR-W6-3 rules that a snapshot which is NULL *or* the empty string is
// ABSENT and falls through, at BOTH closures, and that no TrimSpace is
// introduced at the muster seam — so a whitespace-only snapshot renders as
// written. That cost is not hidden: it is pinned, as frozen row (iii) and
// live row (vii).
//
// ⚠ Every row below runs as its OWN board with ONE member, deliberately.
// A four-row table rendered as one board would let a sort reorder decide
// which cell a failure belongs to, and the certified values are per-row.
package domain

import "testing"

// w6OneRowBoard renders a single visit through the named closure and
// returns the one name the sheet would print. It asserts its own
// arrangement (BUILDER_BRIEF_RULES 6): exactly one member, in one bucket.
func w6OneRowBoard(t *testing.T, frozen bool, v Visit, persons []Person) string {
	t.Helper()
	var b MusterBoard
	if frozen {
		b = w6FrozenBoard([]Visit{v}, persons)
	} else {
		b = w6LiveBoard([]Visit{v}, persons)
	}
	names := w6BoardNames(b)
	if len(names) != 1 {
		t.Fatalf("PRECONDITION: the one-row fixture rendered %d names %s, want exactly 1 — "+
			"this row cannot certify a value it cannot locate (visit %s, person %s, minCrewSize 0)",
			len(names), w6Fmt(names), v.VisitID, v.PersonID)
	}
	if b.Total != 1 {
		t.Fatalf("PRECONDITION: board.Total = %d, want 1 — the visit did not land as an OPEN member", b.Total)
	}
	return names[0]
}

// --- W6-5 TABLE 1: the FROZEN closure -----------------------------------
//
// Kills (m6-1) at row (ii) [and again at row (iii)], (m6-1e) at row (i),
// (m6-5) at rows (i) and (iv), (m6-5b) at row (iii).
//
// ⟦LOCK — H-4, post-lock amendment 2026-09-02. ROW (iii) STRUCK from
// (m6-5)'s kill-list here, the SECOND of the two landings the strike needed.
// [MEASURED, reports/window-build/W6-falsification.md §1.8 item 1 and
// W6-falsification-2.md §1.8, at subtest granularity] applied ALONE, (m6-5)
// reds rows (i) and (iv) ONLY; row (iii) PASSES. TABLE 1 below says so in its
// own cells: on row (iii) the certified answer is `   ` (three spaces, as
// written) and rival A — SQL non-NULL semantics, which IS (m6-5) — is also
// `   `. Identical cells cannot separate the rule from the rival, so the
// booked kill was never producible. The register row was struck at
// plan:6759 (G-8, 2026-09-02) and W6-5's own `Kills:` line at plan:6167 in
// the same pass as this comment; this file was the third landing and had
// not previously been recorded anywhere.
// ⟦was: "(m6-5) at rows (i)/(iii)/(iv)"⟧⟧
//
// EXPECTED RED at f33c4c7: rows (ii) and (iii). The pre-chunk code is
// `name := personName[v.PersonID]; if name == "" { name = v.PersonID }`
// (muster.go:240-243), shared by both boards, which never reads a snapshot:
// row (ii) renders the LIVE "Vega, Anna" where the certified answer is the
// frozen "Vega, Ana", and row (iii) renders "Vega, Ana" where the certified
// answer is the three spaces as written. Rows (i) and (iv) PASS pre-chunk
// (plan §2.5 row 12) and their clean pass is NOT evidence — their evidence
// is (m6-1e) and (m6-5).
func TestW6FrozenClosurePrefersSnapshotThenLiveThenRawID(t *testing.T) {
	rows := []struct {
		row       string
		snapshot  *string // nil == the column is NULL
		liveName  string
		personID  string
		certified string
		why       string
	}{
		{
			row: "(i)", snapshot: w6Str(""), liveName: "Vega, Ana", personID: "p-1",
			certified: "Vega, Ana",
			why: "the MIDDLE arm — adjudication #7's live fall-through. An EMPTY snapshot is ABSENT " +
				"(DR-W6-3), so the frozen closure falls to the LIVE name rather than to the raw id. " +
				"This is the only row that separates the ruled three-arm closure from a TWO-ARM one",
		},
		{
			row: "(ii)", snapshot: w6Str("Vega, Ana"), liveName: "Vega, Anna", personID: "p-2",
			certified: "Vega, Ana",
			why: "the ORDINARY case, and the row whose kill rests on NO absence ruling: the snapshot is " +
				"present and non-empty on any reading, so certified = snapshot != live whatever DR-W6-3 " +
				"decides. It separates the certified rule from the PRE-CHUNK code, which renders the live name",
		},
		{
			row: "(iii)", snapshot: w6Str("   "), liveName: "Vega, Ana", personID: "p-3",
			certified: "   ",
			why: "the WHITESPACE row. DR-W6-3 refuses TrimSpace at the muster seam, and states the cost: a " +
				"whitespace-only snapshot renders AS WRITTEN — a visually blank line on a roll call. That cost " +
				"is pinned here as a literal rather than left as an unexamined consequence. It is the only " +
				"arrangement that can kill a trim introduced at this seam",
		},
		{
			row: "(iv)", snapshot: w6Str(""), liveName: "", personID: "p-4",
			certified: "p-4",
			why: "every arm falls through to the LAST RESORT adjudication #7 says 'must be preserved' — " +
				"still show the id, never drop the person",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			v := w6VisitWithSnapshot(t, "v-"+r.personID, r.personID, r.snapshot)
			carried, present := w6SnapshotOf(t, v)
			got := w6OneRowBoard(t, true, v, w6Persons([2]string{r.personID, r.liveName}))
			if got != r.certified {
				t.Errorf("W6-5 FROZEN row %s: MusterReprint rendered %q, want %q.\n"+
					"  what the row is for: %s\n"+
					"  fixture: snapshot=%s live Name=%q person=%s\n"+
					"  the visit row actually carried %s=%q (present=%v)\n"+
					"  ⚠ if present=false the `%s` field does not exist on Visit yet — that is W6's PRE-CHUNK RED, "+
					"not a fixture defect: the frozen closure fell through to the live name because there was "+
					"nothing to prefer. After the build, present=false means the POPULATE is broken ((m6-1b)); "+
					"present=true with the wrong render means the READ is ((m6-1), (m6-1e), (m6-5), (m6-5b))",
					r.row, got, r.certified, r.why, w6DescribeSnapshot(r.snapshot), r.liveName, r.personID,
					w6VisitSnapshotKey, carried, present, w6VisitSnapshotKey)
			}
		})
	}
}

// --- W6-5 TABLE 2: the LIVE closure -------------------------------------
//
// Rivals A and B are NOT EXPRESSIBLE here: after DR-W6-5's strike the live
// closure consults no snapshot at all. What this table holds is that W6's
// diff leaves muster.go:240-243 doing exactly what it does today, checked at
// the value level for the empty/whitespace class.
//
// Row (vi) DETECTS both of (m6-4)'s arms; row (v) is what tells them APART.
// Row (vii) kills (m6-5b)'s LIVE arm — frozen row (iii) cannot, because the
// two closures can be trimmed independently.
//
// EXPECTED RED at f33c4c7: NONE. All three rows are already true (plan §2.5
// row 4) and their clean pass is NOT evidence. Their evidence is the plants:
// (m6-4) arm A (snapshot-first live closure), (m6-4) arm B (live-then-
// snapshot), (m6-5b) (TrimSpace at the live seam).
func TestW6LiveClosureNeverConsultsTheSnapshot(t *testing.T) {
	rows := []struct {
		row       string
		snapshot  *string
		liveName  string
		personID  string
		certified string
		why       string
	}{
		{
			row: "(v)", snapshot: w6Str("Vega, Anna"), liveName: "Vega, Ana", personID: "p-5",
			certified: "Vega, Ana",
			why: "DIAGNOSIS, not detection: the live name is present, so a snapshot-FIRST closure renders " +
				"\"Vega, Anna\" here while a live-THEN-snapshot one renders the certified answer. A red on " +
				"(vi) alone identifies arm B; a red on both identifies arm A",
		},
		{
			row: "(vi)", snapshot: w6Str("Vega, Bea"), liveName: "", personID: "p-6",
			certified: "p-6",
			why: "DETECTION of BOTH of (m6-4)'s arms: the live name is absent, so any snapshot arm in any " +
				"position renders \"Vega, Bea\" against a certified raw id. ⚠ W6-4 forbids that arm — an " +
				"unreplicated or empty-named person keeps rendering a raw PersonID on the live board, which " +
				"is self-evidently not a name, where a frozen name in the same <li> as every live name is " +
				"marked as nothing at all",
		},
		{
			row: "(vii)", snapshot: w6Str(""), liveName: "   ", personID: "p-7",
			certified: "   ",
			why: "the live arm of (m6-5b): whitespace already renders as written on the live path " +
				"(muster.go:241 tests == \"\", and nothing trims). A TrimSpace introduced at the LIVE seam " +
				"renders \"p-7\" here",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			v := w6VisitWithSnapshot(t, "v-"+r.personID, r.personID, r.snapshot)
			got := w6OneRowBoard(t, false, v, w6Persons([2]string{r.personID, r.liveName}))
			if got != r.certified {
				t.Errorf("W6-5 LIVE row %s: MusterLive rendered %q, want %q.\n"+
					"  what the row is for: %s\n"+
					"  fixture: snapshot=%s live Name=%q person=%s\n"+
					"  ⚠ this row is ALREADY TRUE at f33c4c7 (plan §2.5 row 4). A red here means W6's diff "+
					"CHANGED the live board — which is (m6-4), the \"one edit, both boards\" build, and what "+
					"§1 correction (1) exists to prevent",
					r.row, got, r.certified, r.why, w6DescribeSnapshot(r.snapshot), r.liveName, r.personID)
			}
		})
	}
}

func w6DescribeSnapshot(s *string) string {
	if s == nil {
		return "NULL"
	}
	return "\"" + *s + "\""
}

// --- W6-4: OUTPUT-IDENTITY on the live board ----------------------------
//
// Adjudication #6 (SCHEMA10-ARBITER-VERDICT.md:192): "'Byte-identical
// MusterLive' is unachievable and must not be asserted … The testable
// property is output-identity: assert the live board renders identically
// before and after, BY TEST, not by diff claim."
//
// The "before" side is NOT a diff reading — it is the ordered literal below,
// captured by running this exact fixture against f33c4c7 [MEASURED].
//
// The fixture exercises all THREE of today's live name arms, and every visit
// carries a snapshot that DIFFERS from its certified answer, so a snapshot
// arm added in ANY position changes the output:
//
//	p-a  live "Ash, Ann"  snapshot "Zulu, Zed"     -> "Ash, Ann"   arm (i)
//	p-b  absent           snapshot "Bravo, Bee"    -> "p-b"        arm (ii)
//	p-c  live ""          snapshot "Charlie, Cee"  -> "p-c"        arm (iii)
//
// EXPECTED RED at f33c4c7: NONE — output-identity is already true by
// construction (plan §2.5 row 3). Its evidence is (m6-4), applied ONE ARM AT
// A TIME: arm A the coalesce placed in buildMuster's shared block; arm B the
// live closure given a snapshot arm.
func TestW6LiveBoardRenderIsUnchangedByW6(t *testing.T) {
	visits := []Visit{
		w6VisitWithSnapshot(t, "v-a", "p-a", w6Str("Zulu, Zed")),
		w6VisitWithSnapshot(t, "v-b", "p-b", w6Str("Bravo, Bee")),
		w6VisitWithSnapshot(t, "v-c", "p-c", w6Str("Charlie, Cee")),
	}
	// p-b is deliberately NOT in the roster: the unreplicated-roster gap,
	// muster_test.go:397-466's pinned intent ("want the raw id … fall back to
	// the id, don't drop or blank the row").
	persons := w6Persons([2]string{"p-a", "Ash, Ann"}, [2]string{"p-c", ""})

	// --- the fixture asserts its own arrangement (rule 6) ---------------
	// Three distinct arms are the whole point; if two of them collapse onto
	// the same door this fixture stops separating a snapshot-first build
	// from a live-then-snapshot one.
	idx := personNameIndex(persons)
	if n, ok := idx["p-a"]; !ok || n == "" {
		t.Fatalf("PRECONDITION: arm (i) requires p-a present with a non-empty live Name; got %q present=%v", n, ok)
	}
	if _, ok := idx["p-b"]; ok {
		t.Fatalf("PRECONDITION: arm (ii) requires p-b ABSENT from the roster index — the fixture put it there")
	}
	if n, ok := idx["p-c"]; !ok || n != "" {
		t.Fatalf("PRECONDITION: arm (iii) requires p-c present with an EMPTY live Name; got %q present=%v", n, ok)
	}
	// --- the pinned "before" side [MEASURED at f33c4c7] -----------------
	wantNames := []string{"Ash, Ann", "p-b", "p-c"}
	const wantCompany, wantTrade = "Northwind Drilling", "Rig Crew"

	// --- the snapshot-DIFFERS guard, which now ASSERTS ------------------
	//
	// ⟦LOCK — H-10, post-lock amendment 2026-09-02. THIS LOOP COULD NOT
	// FAIL. It read
	//
	//	for _, v := range visits {
	//		if _, present := w6SnapshotOf(t, v); present { continue }
	//	}
	//
	// — both paths a no-op — while sitting in the fixture-guard position and
	// carrying a comment describing a guard it did not perform. That is the
	// shape BUILDER_BRIEF_RULES' first standing rule names: a check that
	// cannot fail is worse than no check, because it reads as cover.
	// ⟦was: "// Post-build only. Pre-chunk the key is dropped by Unmarshal
	// and this is silently skipped — which is correct: pre-chunk there is no
	// snapshot arm to be tempted by."⟧⟧
	//
	// WHAT IS ASSERTED INSTEAD is the premise this fixture's own header
	// states: "every visit carries a snapshot that DIFFERS from its certified
	// answer, so a snapshot arm added in ANY position changes the output". A
	// snapshot that were EMPTY, or that happened to equal a certified name,
	// would leave (m6-4) arm B alive at that row with the pinned literal
	// still green.
	//
	// ⚠ Compared against EVERY pinned name, not against wantNames[i]: the
	// board's render order is the roster body's, not the fixture's slice
	// order, and a positional pairing would be an unasserted assumption in
	// the very place that exists to remove them.
	//
	// ⚠ PRE-CHUNK IT IS SILENT, deliberately and necessarily: at f33c4c7 the
	// struct has no such field, json.Unmarshal drops the key, `present` is
	// false for all three and the loop passes. W6-4 is ⟦NO PRE-CHUNK RED⟧
	// (plan §2.5 row 3) and a guard that reddened there would contradict the
	// lock. On the DELIVERED tree all three are present, so the guard is
	// live — and it was PRODUCED red by giving p-a the snapshot "Ash, Ann".
	for _, v := range visits {
		snap, present := w6SnapshotOf(t, v)
		if !present {
			continue
		}
		if snap == "" {
			t.Fatalf("FIXTURE DEFECT (W6-4): visit %s (person %s) carries an EMPTY snapshot. DR-W6-3 rules an "+
				"empty snapshot ABSENT, so this row would render identically with and without a snapshot arm and "+
				"(m6-4) arm B would survive here", v.VisitID, v.PersonID)
		}
		for _, w := range wantNames {
			if snap != w {
				continue
			}
			t.Fatalf("FIXTURE DEFECT (W6-4): visit %s (person %s) carries the snapshot %q, which is one of the "+
				"pinned certified names %s. This fixture's whole force is that every snapshot DIFFERS from every "+
				"certified answer; where they coincide, a live board that wrongly rendered the snapshot would "+
				"still match the pinned literal and (m6-4) arm B would survive.",
				v.VisitID, v.PersonID, snap, w6Fmt(wantNames))
		}
	}

	board := w6LiveBoard(visits, persons)
	got := w6BoardNames(board)
	if !w6Equal(got, wantNames) {
		t.Errorf("W6-4 (m6-4): MusterLive rendered %s, want %s — pinned by running this fixture against f33c4c7.\n"+
			"  Adjudication #6 requires output-identity BY TEST: W6's diff must not change what the live board renders.\n"+
			"  A render of a snapshot name at p-b or p-c is (m6-4) arm B (the live closure given a snapshot arm);\n"+
			"  a render of \"Zulu, Zed\" at p-a is arm A (snapshot-first), or the coalesce placed in buildMuster's SHARED block.",
			w6Fmt(got), w6Fmt(wantNames))
	}

	// The grouping the names sit in is part of the pinned output.
	if len(board.Companies) != 1 || board.Companies[0].CompanyName != wantCompany {
		t.Fatalf("W6-4: live grouping changed — companies %+v, want exactly one %q", board.Companies, wantCompany)
	}
	if len(board.Companies[0].Trades) != 1 || board.Companies[0].Trades[0].TradeName != wantTrade {
		t.Fatalf("W6-4: live grouping changed — workgroups %+v, want exactly one %q",
			board.Companies[0].Trades, wantTrade)
	}
	if len(board.Summary) != 0 {
		t.Fatalf("W6-4: live pooling changed — Summary holds %d members, want 0 at minCrewSize 0", len(board.Summary))
	}
	if board.Total != 3 {
		t.Fatalf("W6-4: live Total = %d, want 3", board.Total)
	}
}

// --- W6-8 behavioural half ----------------------------------------------
//
// [[D-61]] / [[D-15]]b: W6 does not change WHO is on the board. Asserted as
// an ORDERED LIST OF LITERAL PERSON NAMES at BOTH boards, never as a count:
// a pinned count is blind to INVERSION — under `if v.IsOpen == 1 { continue }`
// a fixture of one open and one closed visit still totals one.
//
// ⚠ SCOPE, in the criterion's own terms: asserted for the reprint route AS
// IT EXISTS AT f33c4c7; superseded when the historical roster lands under
// [[D-15]]b, and (m6-8) retires with it. The historical roster's membership
// rule is D-15b's — "Still on site at the window's end (an open visit) →
// INCLUDED … A person is excluded only if their presence and the window
// share no instant at all" — and W6 does not build it and asserts nothing
// about it.
//
// EXPECTED RED at f33c4c7: NONE (plan §2.5 row 6 — muster.go:229 already
// reads `if v.IsOpen != 1 { continue }`). Evidence is the two plants:
// the RELAXATION (the continue removed) and the INVERSION.
func TestW6ClosedVisitsStayOffBothBoards(t *testing.T) {
	open := w6VisitWithSnapshot(t, "v-open", "p-open", w6Str("Open, Olive"))
	closed := w6VisitWithSnapshot(t, "v-closed", "p-closed", w6Str("Closed, Cal"))
	closed.IsOpen = 0

	// --- the fixture asserts its own arrangement (rule 6) ---------------
	// An inversion-detecting fixture needs BOTH states present and their
	// names to sort differently, or the ordered literal cannot distinguish
	// relaxation from inversion.
	if open.IsOpen != 1 || closed.IsOpen != 0 {
		t.Fatalf("PRECONDITION: the fixture needs one open (IsOpen=1) and one closed (IsOpen=0) visit; got %d and %d",
			open.IsOpen, closed.IsOpen)
	}
	persons := w6Persons([2]string{"p-open", "Open, Olive"}, [2]string{"p-closed", "Closed, Cal"})
	if idx := personNameIndex(persons); idx["p-closed"] >= idx["p-open"] {
		t.Fatalf("PRECONDITION: the fixture's two names must sort with the CLOSED one first, so a relaxation "+
			"(both rendered) and an inversion (only the closed one) produce different ordered lists; got %q and %q",
			idx["p-closed"], idx["p-open"])
	}

	want := []string{"Open, Olive"}
	visits := []Visit{open, closed}
	for _, tc := range []struct {
		name   string
		frozen bool
	}{{"frozen", true}, {"live", false}} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var b MusterBoard
			if tc.frozen {
				b = w6FrozenBoard(visits, persons)
			} else {
				b = w6LiveBoard(visits, persons)
			}
			got := w6BoardNames(b)
			if !w6Equal(got, want) {
				extra := ""
				switch {
				case w6Equal(got, []string{"Closed, Cal", "Open, Olive"}):
					extra = " — that is the RELAXATION: buildMuster's `if v.IsOpen != 1 { continue }` removed"
				case w6Equal(got, []string{"Closed, Cal"}):
					extra = " — that is the INVERSION: `if v.IsOpen == 1 { continue }`, which a pinned COUNT could not see"
				}
				t.Errorf("W6-8 (m6-8), %s board: rendered %s, want %s%s.\n"+
					"  W6 does not change WHO is on the board. The tempting 'repair' when a W6-3 fixture shows an "+
					"empty board is to relax this predicate; it is not W6's to touch.",
					tc.name, w6Fmt(got), w6Fmt(want), extra)
			}
		})
	}
}
