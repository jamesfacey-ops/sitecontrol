# DECLARED-REDS.md — the declared-red register

**This is the only declared-red register in this project, and it is the only one that is
checked by anything.** A *declared red* is a test that is deliberately left failing to hold a
ruled question open. Until this file existed, the register of them was prose spread across
about twenty-five markdown reports, and **nothing anywhere read it**.

The cost of that, measured in `reports/window-build/S112-REDLIST-AUDIT.md`: 45 named tests
and at least nine entries that named no test at all, hiding **two ghosts** (a name that
resolves to no test), **six declarations still reading "must stay red" for tests that pass**,
and **four reds declared by no register**. The largest single register, `W789-GATE-LEDGER-C.md`,
named **exactly one resolvable test** in its declared-red list; its other four entries were a
count, a literal ellipsis, a pointer to another document, and a filename. *"Nothing can ever
check them — this is why everything below survived."*

## The rule

> **Every row names a test that exists, in the package that declares it.**

No counts. No ellipses. No nicknames. No filenames. No pointers to other documents. No bare
criterion ids. A row that cannot name its test is **not an entry — it is a note**, and it
belongs in the DISPOSALS table below with what it resolves to.

## What checks this file

`internal/web/redlist_test.go` — `TestTheDeclaredRedRegisterNamesOnlyTestsThatExist`. It runs
in the ordinary suite, parses the three tables below, and fails if:

1. any row's `Test` is not an exact Go test identifier;
2. any row with `Observed` ≠ `ABSENT` names a test that **does not exist anywhere in the module**;
3. any row with `Observed` = `ABSENT` names a test that **does** exist (a ghost that came back);
4. any row's test is not declared in the package the row names;
5. any row carries the pair (`Declared`=RED, `Observed`=GREEN) — a live must-stay-red for a
   passing test is refused outright;
6. a `LIVE-RED` row carries no retirement condition as an expression;
7. a `GHOST` row names no `Successor`, or names one that does not exist;
8. a `Site` does not resolve to the function the row names;
9. the `TOTALS:` line disagrees with the rows it counts;
10. a DISPOSALS row resolves to a row id that is not here, or is `UNRESOLVED` with no reason;
11. a markdown file in this module is not classified in the MANIFEST, or an
    `INVENTORY`-class file cites a test that does not exist, or a `NOT-AN-INVENTORY` file's
    pinned unresolvable-citation count has drifted.

**The success test, and it is the whole point:** delete or rename any test named below and the
suite goes RED naming it. Before this file existed, that edit was silent in every inventory
the project had — and the one mechanical ghost detector it did have,
`TestEveryTestNameCitedInACommentActuallyExists`, scans **Go comments**, so it had already
caught one of these two ghosts in `trips.go`, where the comment was fixed and the markdown
was not.

## What does NOT check this file, stated so nobody assumes it does

- ⛔ **The `Observed` column is not verified by the suite.** Checking it needs a whole-tree run,
  and a test that shells out to `go test ./...` from inside `go test ./...` recurses; gating it
  behind an environment variable would make it a test that silently skips, which is the defect
  class next door. That half is `deploy/redlist-state-check.sh`, which takes (or produces) a
  whole-tree `-v` log and reports stale sentinels, unlisted reds and rows that never ran. **Run
  it whenever the tree's failing set changes.** What the suite *can* do without a run is refuse
  the (RED, GREEN) pair outright, so the column cannot claim both at once.
- ⛔ **The `Declared at` and `Stale at` citations are not resolved.** They point into
  `../reports/`, outside this module — an unpinned dependency this checker deliberately does not
  take. Only their *shape* (`DOC.md:LINE`) is enforced.
- ⛔ **This register does not adjudicate anything.** Every disposition below is quoted from the
  record. Where the record contradicts itself the row says so and cites both sides.

## How to read a row

| Column | Meaning |
|---|---|
| `Declared` | what the record says the test's correct state IS: `RED` (must stay red), `GREEN` (declared red once, retired since), `GREEN-BY-DESIGN` (an inverted sentinel — it asserts an ABSENCE and passes while the absence holds), `NONE` (a ghost declares nothing) |
| `Observed` | measured, at the pinned commit: `RED`, `GREEN`, `ABSENT` |
| `Verdict` | `LIVE-RED` · `RETIRED` (green, and a ruling retired it) · `STALE-DECLARATION` (green, and **nothing** in the record retires it — the declaration still reads live) · `INVERTED` · `GHOST` |
| `Retires when` | the condition that closes the row, **as an expression**. Required for every `LIVE-RED`. A sentinel that does not name what retires it outlives its reason — three here did |
| `Stale at` | a still-live record that CONTRADICTS this row. This column is a finding, not an error: it is where the evidence lives |
| `Site` | for a DERIVED declaration, the `file:line` the derivation names. The checker asks which function encloses that line and fails if it is not this row's test |

## Three things this register decides, because the last one did not

**1. A ghost is a row, not a deletion.** Row R29 records a name that was renamed away —
⟦was: "TestARefusedDispatchPostAuthorsNothing"⟧ — while the old name still stood in three
documents as a live "deliberately red" entry. The repair
is **not** to delete the entry — that greens the register by deleting the evidence. It is
`Observed=ABSENT`, `Verdict=GHOST`, and a `Successor` naming the live test. Both directions are
then checked: the dead name must stay dead, and the successor must exist.

**2. A DERIVED declaration is MATERIALISED here.** `SEAM-W8-4` (`W8-criteria-draft.md:4082`) is
a plan-owner LOCK that states a *property* over four *subjects* and derives its members rather
than listing them. It contains no test names at all, so a keyword sweep reports its members as
declared by nobody — which is exactly what happened, and it is why three correctly-locked reds
were filed as "unlisted". **A register can be checkable without being greppable.** The decision
here: the derivation stays in its own document, and this register carries its members **by
name**, one row each, with the derivation's own per-site output in the `Site` column. The
checker then re-runs the derivation's last step — *which function encloses that line* — so a
member that moves is loud rather than silent. A derived declaration is therefore never reported
as missing; it is reported as enumerated, and the enumeration is what gets checked.

**3. A LIVE-DOCUMENT check is a row on ONE axis only.** James's ruling D-75
(`JAMES-DECISIONS.md:3954`, 2026-09-05) classifies the eight W10 desk-document checks —
`TestW10B4IsNotSignableWhileTheFiveDeskDocumentCommandsReturnTheirTodayValues`,
`TestW10TheSheetCarriesTheTwoNewProbesTheRejectionLimitAndThePhaseLine`,
`TestW10TheSheetDoesNotPreAuthoriseIgnoringAStaleToolsOwnDiscriminator`,
`TestW10NoLiveChecklistLinePointerSurvivesInThePlan`, `TestW10TheLivePointerCheckCanRed`,
`TestW10G8sWorkCellCarriesTheRepairRuleAndTheTripwire`,
`TestW10TheG8FalsifierRepliesDifferentlyToTheBeforeAndAfterRows`,
`TestW10TheG8TripwireGrepCanGoGreenOnAPlantedRow` — every call site of `w10DesignDir` in
`internal/store/w10_desk_documents_test.go` — as **LIVE-DOCUMENT checks, never counted as pass or
fail.** Their subject is today's `design/`, so their red is a worklist and their green is not a
property of the code; and they **FAIL rather than skip** when `SITECONTROL_DESIGN_DIR` is unset
(`PRECONDITION UNMET — the check never ran`), which is the reason the classification is safe: they
cannot pretend to have passed. The decision here: this register carries a member **only** where the
record separately declares it on the OTHER axis — today's document being unfinished (R13, R14, R16,
each of whose notes says so) or a retired declaration (R23) — and the other four have no row and get
none. Nothing here folds the eight into a total: `deploy/redlist-state-check.sh` reconciles
declared-RED rows against observed FAILs, and a green member of the class is invisible to it by
construction. Naming the eight in an `INVENTORY` file makes the class a *checked* list — a renamed
member fails the suite. ⚠️ Run the suite with `SITECONTROL_DESIGN_DIR` set, or five members fail
`PRECONDITION UNMET` and read as five unlisted reds — that is the instrument reporting a run that
never ran, not a regression.

## One cost this register imposes, measured rather than argued

⚠️ **[MEASURED] This file adds itself to the W8-6 census's offender list (row R06): 48 files → 49,
`DECLARED-REDS.md (1 hits)`.** W8-6's census walks the filesystem under `app/`, counts
one lowercase term case-insensitively, and this register cannot name W8-6's own test without
containing it. Every avoidable occurrence was removed — the retirement conditions are written
around the word — and **exactly one remains, inside the test name itself**.

⭐ **The collision is not new and the lock already routed it.** `w8_deletions_test.go:222-234`
records the identical case against W8's own test files and routes it: *"either these rows join
W8-6(d-ii)'s table at the lock, or W8-2 / W8-X4 / W8-23(a) must be re-expressed without the
literal — which would make them invisible to the census that is supposed to see everything, and is
the worse of the two."* **This register is the same shape**, so it takes the same route: ⚠️ **either
`DECLARED-REDS.md` joins the allowlist at the lock, or the register stops naming W8-6's test** —
and by the lock's own reasoning the second is worse, because a declared-red register that cannot
name a declared red is not a register. **The allowlist was NOT edited here: that is a locked test
and the row is the plan owner's to add.**

⚠️ **Nothing else moved. [MEASURED]** the test was RED before and is RED after, its own text
declares *"no baseline count is asserted"*, and the eight `internal/web` scanners that could see
the two new files were re-run anchored: 8 RUN, 8 PASS, 0 FAIL.

---

PIN: 4e899da5c73c2d730b99d3b7c80e689808297683
TOTALS: rows=55 live-red=17 retired=28 stale-declaration=3 inverted=1 ghost=6 derived=4

⚠️ **PROVENANCE OF THE `Observed` COLUMN, stated because it is not this register's own
measurement.** Every value comes from two independent whole-tree runs at this exact commit, in two
separate trees, by two other passes: `RED` from `S119-GREENROW.md` §13's failing set (20 names) and
`GREEN` from `S112-REDLIST-AUDIT.md` §13's verbose log (930 top-level `--- PASS` lines).
**[MEASURED] the two logs agree exactly** — S112's 21 failures are S119's 20 plus
`TestW8DerivedRouteSetIsGated`, which S112 excluded because its extracted tree had no `.git` and
which passes here. Neither log carries `[build failed]`, `[setup failed]`, `panic:`, `test timed
out` or `signal: killed`, and both reconcile to 9 packages by name. ⚠️ **That 9 is a RECORD of
those two runs, not a live expectation — do NOT "correct" it to 10.** The module became TEN
packages at G15 (2026-09-09) when `cmd/dupreport` landed; a run *today* reconciles to 10, and
`deploy/redlist-state-check.sh` expects 10. Rewriting the number above would falsify what those two
logs actually contained. ⛔ **The pass that wrote this
register ABANDONED its own whole-tree run** at the coordinator's instruction while the box was
heading for an OOM; it had completed `cmd/dumpcompare`, `internal/config` and `internal/deadline`
only, and nothing here rests on it. **Re-derive the column with `deploy/redlist-state-check.sh`
when the machine is free.**

⭐ **R26, re-measured 2026-09-05 under D-74.** `TestW7DumpCompareSetCarriesVehicle` was RED from
`bbff17e` (S127 struck the side-table class exclusion; the test asserted the struck rule by name and
§219 held it) until it was **corrected** to assert the replacement rule — not deleted, skipped or
weakened; the corrected test was watched FAIL on a planted re-exclusion and PASS on the revert — and
is GREEN again [MEASURED, `reports/witness/d74-d75-exec.md`]. Its row read `GREEN | GREEN | RETIRED`
throughout that interval, which was stale, and `deploy/redlist-state-check.sh` was the instrument
that said so (one unlisted red).

⭐ **R49, 2026-10-04 — RETIRED → GHOST, by deletion under a ruling.** James removed weather
phases ("the phase conditions can be removed they will not effect site control … go ahead and
delete the phase conditions"). R49's test pinned the phase-scope un-ack sweep, and the phase scope no longer exists, so the test was
deleted rather than left asserting a dead mechanism. Per this register's rule 1 the row is kept
as a ghost, not deleted. Its `Successor` is the nearest live guarantee on the same door — a
`review_phase` un-ack is now REFUSED and authors nothing — not a test of the old behaviour.
(`TOTALS`: retired 29 → 28, ghost 5 → 6.)

`TOTALS` is **recomputed** from the rows by the checker — it is never typed.

<!-- REGISTER: BEGIN -->
| ID | Test | Package | Declared | Observed | Verdict | Retires when | Successor | Declared at | Stale at | Site |
|---|---|---|---|---|---|---|---|---|---|---|
| R01 | `TestW10EveryDumpedProjectionIsComparedOrExcludedWithAStatedReason` | `internal/deadline` | RED | RED | LIVE-RED | `dumped_keys \ (compared ∪ excluded_with_a_stated_reason) == ∅` — 2 keys in neither today (`alert_ack_retraction`, `weather_phase`) [MEASURED 2026-09-05, the test's own verdict since the §219(3) repoint]; was 10 until S127 compared the eight side tables (D-74, JAMES-DECISIONS.md:3910), each of the 10 attributed in S110-SENTINEL-GROWTH.md | — | EIGHT-REDS.md:174;W10-tests.md:278 | — | — |
| R02 | `TestW9a3TheRenderedMusterPageIsByteIdenticalAcrossAmendments` | `internal/web` | RED | RED | LIVE-RED | `F4 §7 and T3/T12 stop contradicting each other` — a ruling retires it; editing the test does not | — | EIGHT-REDS.md:24;S104-W9.md:25 | — | — |
| R03 | `TestW9b1MusterBytesAtTheDestination` | `internal/web` | RED | RED | LIVE-RED | `the destination muster's byte-identity criterion is re-ruled` — §82 disposition 4 sentinel | — | S99-MAPORDER.md:302;EIGHT-REDS.md:25 | — | — |
| R04 | `TestW9b1MusterBytesAtTheORIGIN` | `internal/web` | RED | RED | LIVE-RED | `the origin muster's byte-identity criterion is re-ruled` — §82 disposition 4 sentinel | — | S99-MAPORDER.md:302;EIGHT-REDS.md:26 | — | — |
| R05 | `TestW8TradeByIDDecodesOnlyItsOwnTradesRows` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | S104B-DOMAIN.md:30;W5-POSTLOCK-AMENDMENTS.md:18249;S125-DISPOSITIONS.md:453 | — | — |
| R06 | `TestW8NoWorkgroupReferenceSurvives` | `internal/domain` | RED | RED | LIVE-RED | `W8-6's census reaches 0 offending files` — 48 today; the test asserts no baseline, so the number drifts by design | — | S104B-DOMAIN.md:29;S113-FIFTHDOOR.md:95 | — | — |
| R07 | `TestW5eCargoContentChangesNothingDownstream` | `internal/domain` | RED | RED | LIVE-RED | `w5eRequireNoMintedIDs' canonicalisation map covers W7's vehicle entity` | — | S104B-DOMAIN.md:24;W789-GATE-LEDGER.md:436 | — | — |
| R08 | `TestW6AnEmptySnapshotFallsToTheLiveNameAndIsNotAmendmentInvariant` | `internal/domain` | RED | RED | LIVE-RED | `the fixture's one column name is corrected to the post-W8 trade rename` — S104B names it | — | S104B-DOMAIN.md:25;EIGHT-REDS.md:200 | — | — |
| R09 | `TestW6ArrivalAuthoringDecodesNoExtraPersonRows` | `internal/domain` | RED | RED | LIVE-RED | `the arrival dump's SQL stops raising "no such column" on the renamed person column` | — | S104B-DOMAIN.md:26;EIGHT-REDS.md:201 | — | — |
| R10 | `TestW6ArrivalVisitPayloadKeySetIsExactlyTheSeven` | `internal/domain` | RED | RED | LIVE-RED | `the three pre-rename column literals in w6_populate_test.go are amended` — SEAM-W8-4 per-site amendment | — | S104B-DOMAIN.md:27;W8-PASS-A-RULINGS.md:461 | — | `internal/domain/w6_populate_test.go:391` |
| R11 | `TestW8CarrierRegistrationsAreWiredToADispatchClass` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | S104B-DOMAIN.md:28;W8-build.md:72;R11-DISPATCHCLASS.md:14 | — | — |
| R12 | `TestW1AuthoringSignaturesChangedNonBackwardCompatibly` | `internal/store` | RED | RED | LIVE-RED | `the CreateTrade rename is adjudicated (S64-STORE-REDS.md R2) and the two census rows amended` — routed R2, not taken — CORRECTION 2026-09-13 [MEASURED, FALSIFIABILITY-2.md + re-run here]: the declared cause is ALREADY DISCHARGED (`w1_census_shape_test.go`'s CreateTrade/CreateTradeRecordedBy rows carry successor CreateWorkgroup; no CreateTrade complaint fires) — a dead badge on a live red. The row is RED today ONLY on four variadic-tail signatures (`ResumeCheckIn`, `RecordExpectedArrival`, `RecordExpectedArrivalExcluding`, `RecordUnsolicitedArrival`, `w1_census_shape_test.go:343`); this stated reason does not name that cause and satisfying it would not retire the row | — | S113-FIFTHDOOR.md:96;W8-build.md:327 | — | — |
| R13 | `TestW10B4IsNotSignableWhileTheFiveDeskDocumentCommandsReturnTheirTodayValues` | `internal/store` | RED | RED | LIVE-RED | `0 of 6 (document, command) pairs return their today value` — 6 of 6 do — LIVE-DOCUMENT check (D-75, JAMES-DECISIONS.md:3954): never counted as pass or fail; this row is the OTHER axis, today's document unfinished | — | W10-tests.md:368;W7-DOORS.md:469 | — | — |
| R14 | `TestW10TheSheetCarriesTheTwoNewProbesTheRejectionLimitAndThePhaseLine` | `internal/store` | RED | RED | LIVE-RED | `time_source, occurred_at_correct, cannot detect a missed bump and sheet phase are all present` — all four 0 today — LIVE-DOCUMENT check (D-75, JAMES-DECISIONS.md:3954): never counted as pass or fail; this row is the OTHER axis, today's document unfinished — CORRECTION 2026-09-13 [MEASURED here, re-running the test]: `sheet phase` is NOT 0 today (measured phase=1 at `w10_desk_documents_test.go:411`) — that row's sheet half is discharged; the red stands on the other three (`time_source`, `occurred_at_correct`, `cannot detect a missed bump`), each still 0 | — | W10-tests.md:369;W7-DOORS.md:470 | — | — |
| R15 | `TestW10B6sDumpcompareDischargeClauseOneOfThreeBuildPs1BuildsTheTool` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W10-tests.md:371;W7-DOORS.md:471 | — | — |
| R16 | `TestW10G8sWorkCellCarriesTheRepairRuleAndTheTripwire` | `internal/store` | RED | RED | LIVE-RED | `G8's work cell carries "mechanism working"` — the red IS the W10-13 tripwire arm firing — LIVE-DOCUMENT check (D-75, JAMES-DECISIONS.md:3954): never counted as pass or fail; this row is the OTHER axis, today's document unfinished | — | W10-tests.md:374;W7-DOORS.md:472 | — | — |
| R17 | `TestW6VisitProjectionCarriesThePersonSnapshotColumn` | `internal/store` | RED | RED | LIVE-RED | `SEAM-W8-4's per-site lock amendment lands for the visit column set` — owner: the plan owner, discharge: W8's fold | — | W8-criteria-draft.md:4082;W8-PASS-A-RULINGS.md:458 | — | `internal/store/w6_visit_projection_test.go:390` |
| R18 | `TestW6VisitColumnSetIsExactlyTheEleven` | `internal/store` | RED | RED | LIVE-RED | `SEAM-W8-4's per-site lock amendment lands for W6-7(a)'s exact set` — CORRECTION 2026-09-13 [MEASURED here, re-running the test]: a SECOND, undeclared cause is in the measured column set today — `predates_orientation_rule` (SchemaVersion 12) — that this stated reason does not name; landing SEAM-W8-4 alone (the workgroup_snapshot_* rename) would NOT retire this row while that column remains present | — | W8-criteria-draft.md:4082;W8-PASS-A-RULINGS.md:459 | — | `internal/store/w6_visit_projection_test.go:430` |
| R19 | `TestW6VisitAndArrivalVisitCarryTheDeclaredGoShape` | `internal/store` | RED | RED | LIVE-RED | `SEAM-W8-4's per-site lock amendment lands for ArrivalVisit's Go field set` — CORRECTION 2026-09-13 [MEASURED here, re-running the test]: a SECOND, undeclared cause is in the measured field set today — `PredatesOrientationRule` (SchemaVersion 12) — that this stated reason does not name; landing SEAM-W8-4 alone (the WorkgroupID/WorkgroupName rename) would NOT retire this row while that field remains present | — | W8-criteria-draft.md:4082;W8-PASS-A-RULINGS.md:463 | — | `internal/store/w6_visit_projection_test.go:563` |
| R20 | `TestW10TheStalenessRowSeparatesADangerousPreAuthorisationFromAHarmlessOne` | `internal/store` | RED | RED | LIVE-RED | `dangerous_fixture ∈ compareSet ∧ harmless_fixture ∉ compareSet` — TWO guards, not one: `workgroup` (dangerous) ∉ compareSet since the W8 rename (guard w10_desk_documents_test.go:779, the §197 reason) AND `person_amendment` (harmless) ∈ compareSet since bbff17e (guard :797, fires the moment the first is repaired — MEASURED 2026-09-04, §219(4)); the replacement harmless name must be a named exclusion, i.e. the W10-10 sentinel's own subject, so the re-derivation is the row owner's call | — | W10-UNCHECKABLE-AUDIT.md:299;W7-DOORS.md:473 | W10-tests.md:378 | — |
| R21 | `TestW5eNoCargoOnArrivalStructural` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W9-LOCKED-REDS.md:17 | W789-GATE-LEDGER-C.md:50 | — |
| R22 | `TestARefusedPostAuthorsNothing` | `internal/web` | GREEN | GREEN | STALE-DECLARATION | — | — | EIGHT-REDS.md:60;S62-ROUTES.md:1678 | W9-LOCKED-REDS.md:588 | — |
| R23 | `TestW10TheSheetDoesNotPreAuthoriseIgnoringAStaleToolsOwnDiscriminator` | `internal/store` | GREEN | GREEN | STALE-DECLARATION | — | — | W10-tests.md:370 | W10-tests.md:370 | — |
| R24 | `TestW2RouteWalkEveryPOSTRouteClassified` | `internal/web` | GREEN | GREEN | STALE-DECLARATION | — | — | W9a-build.md:100 | W9a-build.md:100 | — |
| R25 | `TestNoPhoneNumberIsAskedForOrKeptAnywhere` | `internal/web` | NONE | ABSENT | GHOST | — | `TestNoPhoneNumberReachesTheMusterPrintOrRoster` | W5-EXPOSURE-ASSESSMENT.md:61;W5d-BUILD-CRITIQUE.md:668 | W5-EXPOSURE-ASSESSMENT.md:61 | — |
| R26 | `TestW7DumpCompareSetCarriesVehicle` | `internal/store` | GREEN | GREEN | RETIRED | — | — | ALIAS-DELETION-REPAIR.md:286;S68D-REFUSAL-CENSUS.md:356;JAMES-DECISIONS.md:3910 | W8-build.md:335 | — |
| R27 | `TestW4CarrierCoverageWithEmptyExemptions` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W9-LOCKED-REDS.md:17;W9b-build.md:168 | W9b-build.md:168 | — |
| R28 | `TestW10TheClockVerdictChannelIsStillAbsentAsW10Dash9Recorded` | `internal/web` | GREEN-BY-DESIGN | GREEN | INVERTED | — | — | S101-CLOCK.md:218;S112-REDLIST-AUDIT.md:687 | — | — |
| R29 | `TestARefusedDispatchPostAuthorsNothing` | `internal/web` | NONE | ABSENT | GHOST | — | `TestARefusedPostAuthorsNothing` | W9-LOCKED-REDS.md:588;EIGHT-REDS.md:19 | W9-LOCKED-REDS.md:588 | — |
| R30 | `TestW10AClockVerdictChannelExistsOnThisTree` | `internal/web` | NONE | ABSENT | GHOST | — | `TestW10TheClockVerdictChannelIsStillAbsentAsW10Dash9Recorded` | EIGHT-REDS.md:22;W10-tests.md:340 | EIGHT-REDS.md:22 | — |
| R31 | `TestMusterLiveGroupsCompanyThenTradeAndOffersAPrintLink` | `internal/web` | GREEN | GREEN | RETIRED | — | — | EIGHT-REDS.md:20 | — | — |
| R32 | `TestRecordedByLandsOnAddOnDemandCompanyTradeAndPerson` | `internal/web` | GREEN | GREEN | RETIRED | — | — | EIGHT-REDS.md:21 | — | — |
| R33 | `TestW6PrintedLineOrderIsInvariantUnderANameAmendment` | `internal/web` | GREEN | GREEN | RETIRED | — | — | EIGHT-REDS.md:23 | — | — |
| R34 | `TestW10TheReconciliationSeesAProjectionRegisteredAtASecondSite` | `internal/deadline` | GREEN | GREEN | RETIRED | — | — | EIGHT-REDS.md:155 | — | — |
| R35 | `TestW6LiveBoardRenderIsUnchangedByW6` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | EIGHT-REDS.md:193 | — | — |
| R36 | `TestW9b1PeerHalfTheDestinationGetsTheTripBack` | `internal/web` | GREEN | GREEN | RETIRED | — | — | W9-RECONCILIATION.md:294 | — | — |
| R37 | `TestW9b2NoSurfaceOffersAReplacementTripSelection` | `internal/web` | GREEN | GREEN | RETIRED | — | — | W9-RECONCILIATION.md:290 | — | — |
| R38 | `TestW9b5TheRetractedArrivalIsRenderedOnTheReviewLanePage` | `internal/web` | GREEN | GREEN | RETIRED | — | — | W9-RECONCILIATION.md:287 | — | — |
| R39 | `TestW5aPersonProjectionColumnsW5X4` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W5d-BUILD-CRITIQUE.md:834;W5c-build.md:230 | W8-build.md:328 | — |
| R40 | `TestW5bPersonProjectionPhoneColumnW5X4` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W5d-BUILD-CRITIQUE.md:835;W5c-build.md:237 | W8-build.md:329 | — |
| R41 | `TestW5cOfficerCannotAuthorFalseThroughTheAmendmentForm` | `internal/web` | NONE | ABSENT | GHOST | — | `TestW5cOfficerCannotAuthorPadOrientationThroughTheAmendmentForm` | W5b-build.md:416 | — | — |
| R42 | `TestW5cNonDegeneracySelfCheckPositiveControl` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W5c-build.md:136 | — | — |
| R43 | `TestW5eCargoNoteColumnStructural` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W5e-tests.md:59 | W8-build.md:336 | — |
| R44 | `TestW5eSchemaChangeDeclaredInsideCreateLiteral` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W5e-tests.md:60 | — | — |
| R45 | `TestW5eCargoNoteStoredByteFaithfully` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | S112-REDLIST-AUDIT.md:209 | W789-GATE-LEDGER-C.md:123 | — |
| R46 | `TestW5eCargoNoteAbsentIsNullAndDistinctFromEmpty` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | S112-REDLIST-AUDIT.md:209 | W789-GATE-LEDGER-C.md:123 | — |
| R47 | `TestW5eCargoNoteWireLiteralIsPinned` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | S112-REDLIST-AUDIT.md:210 | W789-GATE-LEDGER-C.md:123 | — |
| R48 | `TestW5eDispatchRefusalSetUnchanged` | `internal/store` | GREEN | GREEN | RETIRED | — | — | W7-LENS-crosschunk.md:330;S68D-REFUSAL-CENSUS.md:165 | W8-build.md:337 | — |
| R49 | `TestW9c6TheWidePhaseSweepReachesEveryEffectivePhaseAckOnTheTrip` | `internal/web` | NONE | ABSENT | GHOST | — | `TestFix3StaleWeatherFormFieldsAreRefusedAndAuthorNothing` | W10-criteria-draft.md:640 | — | — |
| R50 | `TestW9c9OnePostAuthorsExactlyOneRetractionPerEFFECTIVEAck` | `internal/web` | GREEN | GREEN | RETIRED | — | — | W10-criteria-draft.md:641 | — | — |
| R51 | `TestEmptyAndPlaceholderVehicleIDsAreRefusedForDistinctStatedReasons` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W7-LENS-seams.md:990 | — | — |
| R52 | `TestGateCheckInRefusesAPlaceholderVehicleID` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W7-LENS-seams.md:987 | — | — |
| R53 | `TestGateCheckInStillAcceptsARealVehicleID` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W7-LENS-seams.md:988 | — | — |
| R54 | `TestPlaceholderVehicleIDIsRefusedOnEveryVehicleBearingAuthoringPath` | `internal/domain` | GREEN | GREEN | RETIRED | — | — | W7-LENS-seams.md:989 | — | — |
| R55 | `TestW5cPersonProjectionCarriesPadOriented` | `internal/store` | NONE | ABSENT | GHOST | — | `TestW5cPersonProjectionCarriesNoPadOrientedColumn` | W8-build.md:330;W7-DOORS.md:464 | W8-build.md:330 | — |
<!-- REGISTER: END -->

## DISPOSALS — every entry the old registers carried that named no test

These are kept because they are the finding. Each one is an entry that stood as a declared red
while naming a count, an ellipsis, a filename, a nickname, a pointer or a criterion id. Where
one resolves, it resolves to rows above. Where it does not, it says `UNRESOLVED` and says why —
a register may not close an entry by guessing what it meant.

<!-- DISPOSALS: BEGIN -->
| Entry as written | Source | Kind | Resolves to | Note |
|---|---|---|---|---|
| "3 in `internal/web`: the W9 trio" | W789-GATE-LEDGER-C.md:40 | COUNT | R02 R03 R04 | A count and a nickname. The three are named in `EIGHT-REDS.md:24-26` and nowhere the gate was handed. |
| ⟦was: "TestW9a3… now reports TWO arms"⟧ | W789-GATE-LEDGER-C.md:43 | ELLIPSIS | R02 | A literal ellipsis inside the name. `grep` for it in the tree returns nothing. |
| "`W9-LOCKED-REDS.md` carries one sentinel that MUST STAY RED" | W789-GATE-LEDGER-C.md:50 | POINTER | R21 | The pointed-at document holds two entries and neither is the one meant; the sentinel is GREEN and was ruled retired at §38. |
| "one whose retirement condition has now been MET (§160)" | W789-GATE-LEDGER-C.md:51 | POINTER | R05 | S112 Finding 4: §160 is in `S104B-DOMAIN.md`, a document the ledger never cites. Following the pointer lands on the wrong two entries. |
| "`w5e_cargo_note_roundtrip_test.go` is BOOKED to go RED" | W789-GATE-LEDGER-C.md:123 | FILENAME | R07 R45 R46 R47 | A file, in `internal/domain`, four lines below a `internal/store` entry — so it reads as store. The file holds four tests and only one of them is red. |
| "`w5e_cargo_note_roundtrip_test.go` will go RED on `w5eRequireNoMintedIDs`" | W789-MUTANT-GATE-BRIEF.md:81 | FILENAME | R07 | A second, independent copy of the same booking, naming the file and a helper. |
| "W6 visit projection — 3 (1 precondition), store" | EIGHT-REDS.md:136 | COUNT | R17 R18 R19 | A count row with no names. It is the entry that hid three of S112's four "unlisted" reds — Finding 0 producing Finding 9. |
| ⟦was: "TestW10B4… · TestW10TheSheetCarries… · TestW10B6s… · TestW10G8s…"⟧ | W8-build.md:338 | ELLIPSIS | R13 R14 R15 R16 | NOT in S112's list, and found here. `W8-build.md` §5.1 is the sixteen-row store table S119 used to overturn S112's Finding 9 — and four of its rows are ellipses. |
| "the store suite now carries two standing reds" | W4-tests.md:280 | COUNT | UNRESOLVED | A count with a condition ("until the builder lands the side table") that was met long ago. No document names the two, and reconstructing them from a W4-era suite is not something this register will guess at. |
| "The store suite now carries two standing reds" | W5a-tests.md:393 | COUNT | UNRESOLVED | Same shape, same reason. The two are PROBABLY R39 and R40 — and probably is exactly the word a register may not contain. |
| "W5b's own standing reds total 11" | W5b-tests.md:153 | COUNT | UNRESOLVED | The line adds "and every one is named in §2" — a pointer to a section, which is a second unnameable entry, not a resolution. |
| "W5c's own standing reds total 8" | W5c-tests.md:724 | COUNT | UNRESOLVED | Same shape as the W5b row. |
| "§38 says in terms 'the test stays red until the derivation is implemented'" | S98-OBLIGATIONS.md:125 | POINTER | R21 | A ruled, known red carried as a pointer at §37.3/§38. The derivation was implemented; the pointer still reads live. |
| "the criterion will red at W8 and the re-pin is FORESEEABLE" | W7-criteria-draft.md:1884 | CRITERION-ID | R26 | Recorded against `SEAM-W7-17` rather than against a test name. |
| "W8-9(c) stays red for reasons no compiler reports" | ALIAS-DELETION-SCOPE.md:331 | CRITERION-ID | UNRESOLVED | A criterion id with no test name. W8-9(c)'s test-side sites are not enumerated anywhere this sweep reached, so the entry stays open rather than being guessed at. |
| "The test remains RED overall — correctly" | S56-PLANT-HARNESS.md:38 | NICKNAME | UNRESOLVED | Names no test at all — "the test". The surrounding section is about the W6 web-merge plant harness, and no test of that section is in the freeze's failing set, so the entry cannot be closed without re-deriving which test it means. |
<!-- DISPOSALS: END -->

## MANIFEST — every markdown file in this module, classified

*"Nothing enumerates the INVENTORIES"* is the fifth-ranked cause in S112 §15: it found six
declared-red sources, then twenty-five. **The list of lists was short by the same mechanism the
lists were.** Inside this module that list is now closed: every `.md` here must appear below, and
adding one without classifying it fails the suite.

`INVENTORY` files are citation-checked in full — every test name in them must exist.
`NOT-AN-INVENTORY` files are historical build reports; they are **not** repaired by editing them,
because a report is a record of what was true when it was written. What is pinned instead is the
**count of names in them that no longer resolve** — the exemption's cost, printed and checked
every run, so it cannot drift in either direction unnoticed.

**A `NOT-AN-INVENTORY` row reading `0` is ambiguous, and the two readings are not the same
fact.** A **real zero**: the file names one or more `Test…` citations — the same
`\bTest[A-Z][A-Za-z0-9_]{4,}` shape `TestTheDeclaredRedRegisterNamesOnlyTestsThatExist` matches,
outside any `⟦was: …⟧` span — and every one of them currently resolves to a test that exists;
the checker is re-proving that on every run, and the count would move the moment a cited test
were renamed or deleted. A **vacuous zero**: the file names no such citation at all, so `0` holds
by there being nothing to fail, not by anything being confirmed — it cannot move no matter what
happens to the test suite, checker included, and it is guarding nothing. The table cannot show
you which is which; a `0` reads the same either way. To tell them apart for any one row, count
that file's `Test…`-shaped citations yourself, with the same exclusion the checker applies — zero
citations is the vacuous case, one or more (still pinned at `0`) is the real one.

<!-- MANIFEST: BEGIN -->
| File | Class | Unresolvable citations |
|---|---|---|
| `DECLARED-REDS.md` | INVENTORY | 0 |
| `REPORT.md` | NOT-AN-INVENTORY | 1 |
| `deploy/BACKUP-RESTORE.md` | NOT-AN-INVENTORY | 0 |
| `deploy/demo-walkthrough.md` | NOT-AN-INVENTORY | 0 |
| `design/sync-protocol-v1.md` | NOT-AN-INVENTORY | 0 |
| `reports/build-c11.md` | NOT-AN-INVENTORY | 2 |
| `reports/build-destinations.md` | NOT-AN-INVENTORY | 0 |
| `reports/fix-attribution.md` | NOT-AN-INVENTORY | 2 |
| `reports/fix-observability.md` | NOT-AN-INVENTORY | 0 |
| `reports/fix-officer-default.md` | NOT-AN-INVENTORY | 8 |
| `reports/fix-replay.md` | NOT-AN-INVENTORY | 0 |
| `reports/fix-testholes2.md` | NOT-AN-INVENTORY | 3 |
<!-- MANIFEST: END -->

---

*fix3, 2026-10-04: `reports/fix-officer-default.md` 4 → 8 and `reports/fix-testholes2.md` 0 → 3 —
both historical reports cite weather-phase tests deleted with weather phases (James's ruling of
that day). The reports are records and are not edited; the pins move by exactly the deleted
names.*

*Provenance: `reports/window-build/S122-INVENTORY.md`, repair-queue row H. The findings this
register carries forward are `S112-REDLIST-AUDIT.md` (the audit, both directions) and
`S119-GREENROW.md` (the definitive twenty failing tests at the freeze, each with a disposition).*
