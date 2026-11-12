// dumpcompare — the §13.3 step-6 dump-and-compare harness (chunk-plan C12).
//
// Reads two saved dump FILES and diffs the REPLICATED projections only:
// trips, visits, reference data + aliases.
//
// ⚠️ IT NO LONGER FETCHES OVER THE NETWORK, AND MUST NOT BE GIVEN THAT BACK.
// It used to pull GET /debug/projection-dump from each site. D-56
// (2026-09-01) removed that route: it was unauthenticated, outside
// cfg.DevRoutes, and served every projection verbatim to any TCP caller, so
// the verification instrument was also an operational data-dump surface.
// Produce each side's file ON its own post instead —
//
//	sitectl -config /opt/sitectl/site.toml -dump > /tmp/dump-<x>.json
//
// — carry the two files to one machine over the channel that already
// deploys the binary, and run this tool on them. The bytes are identical to
// what the route served (same Store.DumpProjections, same registered set,
// nothing suppressed), so the comparison this tool performs is unchanged.
// An http:// or https:// argument is refused loudly rather than fetched;
// see load(). That whitelist is the design's, verbatim
// (§13.3 step 6): locally derived deadline/sync/session state is excluded
// — deadline_capture and friends are already structurally absent from the
// dump (store/local.go), and the replicated-but-deadline-owned alert_ack
// fold is excluded here because §13.3's compare set names trips, visits,
// and reference data + aliases ONLY. This is explicitly NOT the deferred
// anti-entropy feature: it is a demo-day assert, not a repair mechanism.
//
// Keys present in a dump but not in the whitelist are reported as IGNORED,
// loudly, never diffed and never silently dropped — so a future projection
// that diverges by design cannot fail the demo, and a future projection
// that should converge cannot hide (the operator sees its name every run).
//
// Exit codes: 0 = compared set identical; 1 = a compared projection
// differs (or is present on only one side); 2 = usage/fetch/parse error,
// a VACUOUS run, OR an INCOMPLETE one (both below). The distinction
// matters on demo day: 1 means the §13.3 assert FAILED, 2 means the
// assert never ran.
//
// ⛔ VACUOUS RUNS ARE REFUSED ⟦gate B-6, 2026-09-06⟧. D-85 EXECUTION
// (reports/JAMES-DECISIONS.md, 2026-09-05) MEASURED this tool printing
// "IDENTICAL: 12" over two dumps of EMPTY databases — `sitectl -dump` had run
// with CWD "/" against a relative db_path and dumped nothing, twice, and
// nothing equals nothing. The harness (dc.ps1) was repaired at the CWD; the
// tool was not, and the changeover sheet still told the operator, at 4am,
// that "two empty databases compare IDENTICAL and the tool exits 0 — the
// check is yours". W10-2 requires IDENTICAL "over a NON-ZERO row count", so
// the tool now owns that check: when every compared projection is empty on
// BOTH sides (or none is present on both sides), the verdict word is VACUOUS,
// the exit is 2, and the word IDENTICAL is never printed. One side empty and
// the other populated is a real DIFF (exit 1), not a vacuous run.
//
// Every run also prints a `compared:` line — how many compare-set
// projections were present in both dumps, the row totals each side, and how
// many were absent from both — which is W10-2(c)'s reconciliation
// (`len(compareSet) − absent-from-both = actually compared`) performed by the
// tool rather than left to the operator.
//
// ⛔ INCOMPLETE RUNS ARE REFUSED ⟦G17, 2026-09-09, James's ruling⟧. Until
// today the sentence here read "the verdict line's count is still
// len(compareSet) … it is left exactly as pinned", and that was true: a
// compare-set projection absent from BOTH dumps warned on stderr, did NOT
// set `identical = false`, and the tool printed "IDENTICAL: 24 compared
// projections match" at exit 0 having compared 22 [MEASURED 2026-09-09 at
// 48a85b5]. The verdict word is now withheld: absent-from-both means the
// assert did not run over the whole compare set, so the tool prints
// INCOMPLETE, names the projections, and exits 2.
//
// ⚠ W10-2(c) does NOT require this ("This criterion does not require the
// tool to be changed") but explicitly leaves it to the plan owner, who
// took it. ⛔ The criterion's own CONTROL still says "observe exit 0" and
// is now unreproducible against this binary — it needs amending, and that
// is a DOCUMENT change nobody has made yet. See
// reports/witness/G17-HARDEN-VERDICT.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	// ⟦LOCK — §67, 2026-09-03⟧ the compare set is DERIVED FROM THE REGISTRY, so
	// this tool must know the registry the same way `sitectl -dump` does.
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// compareSet is §13.3's compare set, mapped onto the registered projection
// names (internal/domain/register.go): the trip family (raw folds plus the
// deterministic trip_state/arrival_state recomputes — fold OUTPUT identity
// is exactly what the C3a⇢C6 verify edge checks), the visit family, the
// three reference-data classes.
//
// ⚠⚠ THE STALE-ENTRY DIRECTION IS A FALSE GREEN AND IT IS MEASURED, NOT
// PREDICTED. A key left here for a projection that no longer exists is
// missing from BOTH dumps, hits the !okA && !okB arm, WARNS TO STDERR, the
// loop continues, and the tool prints "IDENTICAL: … compared projections
// match" AND RETURNS 0 — the two-post determinism check passes while counting
// a projection that is gone. So W8's two deleted entries came out of this
// list in the same change that deleted them.
//
// ⚠⚠ AND THE MISSING-ENTRY DIRECTION IS THE OTHER ONE, ALSO MEASURED, AND
// NOTHING ELSE CATCHES IT. A projection never added here falls into
// `ignored`, prints "ignored (not in the §13.3 compare set)" to STDOUT, and
// STILL EXITS 0 — the new register is NEVER COMPARED BETWEEN POSTS. The stale
// keys are caught by W8's byte censuses; an ABSENT key fires no census at
// all, and both dumps compare identically whether or not it is here. That
// asymmetry is why `trade` being on this line is asserted rather than assumed.
// ⟦LOCK — §67, 2026-09-03⟧ ⭐ THE COMPARE SET IS DERIVED FROM THE REGISTRY.
// ⟦was: "var compareSet = []string{"⟧  (a hand-kept list of 12 against a
// registry of 23; the full superseded literal is reproduced at the end of this
// comment so the supersession is legible.)
//
// ⛔ WHY IT COULD NOT STAY A LIST. [PRODUCED 2026-09-03, S64 R1] two posts made
// to genuinely DISAGREE about a projection outside the hand-kept set still
// printed "IDENTICAL: 12 compared projections match" and EXITED 0 — and the
// test that pins this set stayed green, because it pins the twelve that ARE
// compared and says nothing about the eleven that are not.
//
// ⚠ IN A TWO-POST SYSTEM WHOSE ENTIRE PURPOSE IS THAT THE POSTS AGREE, A LIST
// LIKE THAT GOVERNS WHETHER TWO SITES ARE ALLOWED TO DISAGREE. A projection
// registered tomorrow was not compared, and nothing anywhere said so ⟦§67 R1⟧.
//
// ⭐ THE INVERSION: a registered projection is now compared BY DEFAULT, and
// EXCLUDING one is a deliberate act that must state its reason. The failure
// mode moves from "silently uncompared" to "loudly listed below".
//
// ⚠ THE REGISTRY IS BOTH REGISTER CALLS, exactly as `sitectl -dump` does them
// (cmd/sitectl/main.go:45,51) — `deadline.Register` registers `alert_ack` and
// `alert_ack_retraction` as REAL PROJECTIONS, not as a local schema. Deriving
// from the DUMPS instead would have swept those INTO the compare set and broken
// the deliberate site-local exclusion `main_test.go` asserts.
//
// ⚠ THE STALE-ENTRY AND MISSING-ENTRY DIRECTIONS, both previously recorded
// here, are now structurally impossible in one direction and caught in the
// other: a key cannot be stale, because the set IS the registry; and a
// registered projection missing from BOTH dumps still warns (old binary),
// because the tool knows what should exist independently of what the dumps say.
//
// THE SUPERSEDED LITERAL, PRESERVED IN ORDER:
// ⟦was: "\t\"company\", \"workgroup\", \"person\", // reference data"⟧
// ⟦was: "\t\"ref_alias\",                                                          // aliases"⟧
// ⟦was: "\t\"trip\", \"trip_occupant\", \"trip_arrival\", \"trip_cancel\", \"trip_state\", // trips"⟧
// ⟦was: "\t\"arrival_state\",           // trips (arrival correlation fold)"⟧
// ⟦was: "\t\"visit\", \"visit_checkout\", // visits"⟧
//
// ignoredProjections is the explicit, justified exclusion list. ⚠ EVERY ENTRY
// CARRIES ITS REASON, and an entry naming a projection that is NOT registered
// is a HARD FAILURE (the SHRINK TOOTH in buildCompareSet) — otherwise this list
// rots exactly the way the one it replaced did, one layer down.
var ignoredProjections = map[string]string{
	// ⟦LOCK — §82, 2026-09-03⟧ ⭐ THE SIDE TABLES ARE NO LONGER NAMED HERE.
	//
	// ⛔ THE BREACH THIS FIXES WAS MINE. §67 replaced a hand-kept compare set
	// with `registry minus exclusions`, and wrote the exclusions as a map keyed
	// BY SIDE-TABLE NAME. Three standing architectural scans caught it —
	// W3-14 (m20), W4-13b and W5d-15 (m9) — all three saying the same thing:
	// "package(s) outside internal/domain read the side table or its
	// event-type constants — every consumer resolves through the resolver /
	// composed read path, NEVER THE TABLE."
	//
	// ⚠ THE SCANS ARE RIGHT AND THIS FILE WAS WRONG. Their filter is lexical
	// (go/scanner, IDENT and STRING only, comments never emitted) and their
	// string-literal arm is DELIBERATE — a consumer that reads a side table
	// does so through a SQL string. Their allowlist is EMPTY by design:
	// "extensions are gate rulings, not edits made in passing." ⭐ Three
	// independent checks, written before this breach existed, caught it.
	//
	// ⚠ AND THE INVARIANT IS ABOUT NAMING, NOT ONLY QUERYING. This tool never
	// read a row from these tables — it named them to EXCLUDE them. That is
	// still the coupling the invariant forbids: a consumer that knows the
	// physical names of internal/domain's side tables breaks when they change,
	// which is precisely the dependency "never the table" exists to prevent.
	//
	// ⭐ SO THE EXCLUSION IS NOW A PROPERTY, NOT A LIST — see
	// ignoredProjectionClasses below. This is the same move §67 made one level
	// up (derive the compare set, do not enumerate it), applied to the thing
	// §67 itself left enumerated.
	//
	// ⟦POINTER — §78's pointer rule, 2026-09-03⟧ EIGHT KEYS WERE STRUCK HERE AND
	// THEY CANNOT BE WRITTEN DOWN AT THIS SITE: this file is the one the guard
	// scans, so preserving the superseded lines here would re-commit the breach
	// inside the note explaining it.
	//
	// ⚠ THEY ARE PRESERVED VERBATIM, WITH THEIR REASON STRINGS, AT
	// reports/window-build/S82-PASS2-BOUNDARY.md §P3. ⛔ Do not delete that
	// section: a superseded line surviving in NEITHER place is the silent
	// coverage loss §78 exists to prevent.
	//
	// Each was a base projection's name plus one of the three class suffixes
	// below. ⛔ NONE OF THE EIGHT IS EXCLUDED ANY MORE: S127 (2026-09-04) struck
	// the class rule, so all eight are COMPARED between the posts — the
	// measurement that forced it is recorded at ignoredProjectionClasses below.
	// ⭐ They are still not NAMED here, and that is what §82 was protecting: a
	// projection compared BY DEFAULT needs no name at this site at all, so the
	// naming invariant is served better now than it was by the class rule.
	//
	// What remains named individually is what belongs to NO class:
	// ⟦was: "weather_phase": "named in DR-W7-9's precedent list as absent from
	// the landed compare set at W7" — the projection no longer exists: weather
	// phases were removed, James 2026-10-04⟧
	// ⚠ SITE-LOCAL BY DESIGN, and this was an ACCIDENT before §67 rather than a
	// decision: these two sat outside the old hand-kept list only because nobody
	// typed them. cmd/dumpcompare/main_test.go's
	// TestNonWhitelistedDivergenceIsIgnoredButNamed asserts a diverging
	// `alert_ack` must NOT fail the run — "deadline/sync/session state is
	// site-local by design".
	"alert_ack":            "site-local by design: acknowledgment is per-post operator state, not replicated content",
	"alert_ack_retraction": "site-local by design: the retraction half of alert_ack, same reasoning",
}

// ignoredProjectionClasses excluded a CLASS of projection by the suffix its
// name carries, so this consumer never had to know any individual side table's
// name. ⟦LOCK — §82, 2026-09-03⟧ ⛔ ITS THREE MEMBERS ARE STRUCK (S127,
// 2026-09-04) AND THE MAP IS NOW EMPTY: every side table is COMPARED between
// the two posts, like every other registered projection.
//
// ⛔ WHY THEY WENT: THE STATED REASON WAS FALSE FOR ALL THREE SUFFIXES, and
// that is MEASURED, not argued — four real post-pairs, real `sitectl -dump`
// files, this real binary (reports/window-build/S126-RETRACTION.md):
//
//   - `_retraction` said the rows are "read through the resolver". The fold's
//     own header (internal/domain/arrival_unref.go) says the opposite in
//     terms: they are "a FOURTH RAW INPUT … never an effective read".
//   - `_amendment` and `_correction` said "the composed read is over the base
//     projection, which IS compared". The composed read is over the base
//     projection ∪ THE SIDE TABLE, and only the base half was compared.
//
// ⛔ AND THE CONSEQUENCE WAS PRODUCED, NOT PREDICTED. With the raw `person`
// projection BYTE-IDENTICAL on both posts, domain.Persons — the composed read
// behind the muster, the gate, the intake and the roster — returned "Robert
// Stone" at one post and "Bob Stone" at the other, while this tool printed
// "IDENTICAL: 12 compared projections match" and EXITED 0. A second
// construction had the two posts render different attributions for the same
// withdrawn arrival reference, also at exit 0.
//
// ⚠ THERE WAS NO SITE-LOCAL BASIS EITHER. These tables carry site_id,
// node_epoch, seq and the amended facts; `site_id` is WHICH POST AUTHORED the
// amendment — a replicated fact, not per-post operator state. Nothing in them
// is site-local, so there is no legitimate reason for them to differ between
// posts, which is precisely the condition for comparing them.
//
// ⭐ THE MAP IS KEPT, EMPTY, AND THAT IS DELIBERATE. §82's MECHANISM was
// right — a class rule excludes by PROPERTY, so this consumer names no side
// table and the W3-14 / W4-13b / W5d-15 naming invariant is not re-breached.
// What was wrong was its CONTENT. A future class exclusion that can state a
// TRUE reason belongs here, and this is the only shape in which one can be
// written without naming a table. The SHRINK TOOTH in buildCompareSet still
// holds every entry ever added to it.
//
// ⚠ THREE TEST-SIDE READERS TREAT AN EMPTY CLASS MAP AS INSTRUMENT-BROKEN
// (internal/deadline/w10_compare_set_test.go, internal/store/w10_dumpcompare_
// verdict_test.go, internal/store/w7_sentinel_scan_test.go), and two standing
// assertions — W7-12(e) PROPERTY 4 arm 2 and PROPERTY 5 — say in terms that
// the amendment side tables must be OUT. Those reds are the CHANGE'S OWN
// CONSEQUENCE and are REPORTED, not tuned away: nothing in any test file was
// edited by this change. See reports/window-build/S127-CLASSFIX.md.
// ⟦D-74, 2026-09-05⟧ Both W7-12(e) assertions were since CORRECTED under James's
// ruling D-74 (reports/JAMES-DECISIONS.md) to assert the side tables ARE
// compared, and the W7 reader now refuses an unreadable or unrecorded-empty
// class map rather than treating "empty" as broken; the two W10 readers were
// repointed at §219(3). No reader reads an empty map here as a fault today.
//
// ⟦was: "\t\"_amendment\":  \"amendment side tables (DR-W7-9): the composed read is over the base projection, which IS compared\","⟧
// ⟦was: "\t\"_correction\": \"correction side tables: same class — the composed value is read through the resolver, never the table\","⟧
// ⟦was: "\t\"_retraction\": \"retraction side tables: same class as the corrections, read through the resolver\","⟧
var ignoredProjectionClasses = map[string]string{}

// w67Ignored answers "is this projection excluded from the compare set" — by
// name for the individual exclusions, by CLASS SUFFIX for any class rule.
// ⟦LOCK — §82, 2026-09-03⟧ Named exclusions are checked first, and that order
// mattered: `alert_ack_retraction` ALSO matched the `_retraction` class, and
// its real reason is site-locality, not that it is a side table.
//
// ⛔ S127 STRUCK THAT CLASS, so the NAMED entry is now the ONLY thing keeping
// `alert_ack_retraction` out of the compare set. It must not be deleted on the
// theory that a class rule covers it — there is no longer any class rule, and
// removing the name would start comparing per-post operator state.
func w67Ignored(name string) bool {
	if _, ok := ignoredProjections[name]; ok {
		return true
	}
	for suffix := range ignoredProjectionClasses {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// registeredProjections is the registry this binary knows, built the way
// `sitectl -dump` builds it. ⟦LOCK — §67, 2026-09-03⟧
//
// An empty, freshly-registered store dumps exactly the registered projection
// NAMES, so no new exported API is needed on store or domain to ask "what is
// registered?". `:memory:` is used deliberately: gate B-4 runs this during an
// outage and it must not require a writable directory.
func registeredProjections() ([]string, error) {
	st, err := store.Open(":memory:")
	if err != nil {
		return nil, fmt.Errorf("building the projection registry: %w", err)
	}
	defer st.Close()
	if err := domain.Register(st); err != nil {
		return nil, fmt.Errorf("building the projection registry (domain): %w", err)
	}
	if err := deadline.Register(st); err != nil {
		return nil, fmt.Errorf("building the projection registry (deadline): %w", err)
	}
	b, err := st.DumpProjections()
	if err != nil {
		return nil, fmt.Errorf("building the projection registry (dump): %w", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("building the projection registry (decode): %w", err)
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// buildCompareSet is the registry minus the stated exclusions.
// ⟦LOCK — §67, 2026-09-03⟧
//
// ⚠ THE SHRINK TOOTH IS HERE AND IT IS A HARD FAILURE, not a warning. An ignore
// entry naming a projection that is no longer registered means the exclusion
// list has rotted — and a rotted exclusion list is how this defect would come
// back one layer down. ⚠ It must NOT be satisfied by dropping the entry
// silently: a projection that was deliberately excluded and has now vanished is
// a change someone must look at.
func buildCompareSet() ([]string, error) {
	registered, err := registeredProjections()
	if err != nil {
		return nil, err
	}
	inRegistry := make(map[string]bool, len(registered))
	for _, n := range registered {
		inRegistry[n] = true
	}
	var dead []string
	for name := range ignoredProjections {
		if !inRegistry[name] {
			dead = append(dead, name)
		}
	}
	// ⟦LOCK — §82, 2026-09-03⟧ the SHRINK TOOTH extended to the class rules: a
	// suffix that matches nothing registered is a dead rule, and a dead rule is
	// how this list would rot in its new shape.
	for suffix := range ignoredProjectionClasses {
		matched := false
		for _, n := range registered {
			if strings.HasSuffix(n, suffix) {
				matched = true
				break
			}
		}
		if !matched {
			dead = append(dead, "class "+suffix+" (matches no registered projection)")
		}
	}
	if len(dead) > 0 {
		sort.Strings(dead)
		return nil, fmt.Errorf("ignoredProjections names %d projection(s) that are NOT registered: %s\n"+
			"  A stale exclusion is a rotted list, which is the defect deriving this set exists to close.\n"+
			"  Do not delete the entry without looking: a projection that was DELIBERATELY excluded and has\n"+
			"  now vanished is a change that needs a ruling, not a tidy-up",
			len(dead), strings.Join(dead, ", "))
	}
	// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "\t\tif _, ignored := ignoredProjections[n]; !ignored {"⟧
	out := make([]string, 0, len(registered))
	for _, n := range registered {
		if w67Ignored(n) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: dumpcompare <file-a> <file-b>")
		fmt.Fprintln(stderr, "  each argument is a path to a saved projection dump, produced ON")
		fmt.Fprintln(stderr, "  its own post with:")
		fmt.Fprintln(stderr, "    sitectl -config /opt/sitectl/site.toml -dump > dump-<x>.json")
		fmt.Fprintln(stderr, "  (there is no dump URL to fetch — see D-56)")
		return 2
	}
	dumpA, err := load(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "dumpcompare: %s: %v\n", args[0], err)
		return 2
	}
	dumpB, err := load(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "dumpcompare: %s: %v\n", args[1], err)
		return 2
	}

	// ⟦LOCK — §67, 2026-09-03⟧ ⟦was: "\tinSet := map[string]bool{}"⟧ — the set is
	// built from the registry now, and a failure to build it is FATAL rather
	// than a fallback to some smaller list. ⚠ A tool that cannot establish what
	// SHOULD be compared must not report on what does compare.
	compareSet, err := buildCompareSet()
	if err != nil {
		fmt.Fprintf(stderr, "dumpcompare: %v\n", err)
		return 2
	}
	inSet := map[string]bool{}
	for _, k := range compareSet {
		inSet[k] = true
	}

	// Ignored keys: union across both dumps, so a key that exists on only
	// one side and is outside the compare set is still visible by name.
	ignored := map[string]bool{}
	for k := range dumpA {
		if !inSet[k] {
			ignored[k] = true
		}
	}
	for k := range dumpB {
		if !inSet[k] {
			ignored[k] = true
		}
	}
	if len(ignored) > 0 {
		names := make([]string, 0, len(ignored))
		for k := range ignored {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Fprintf(stdout, "ignored (not in the §13.3 compare set): %s\n", strings.Join(names, ", "))
	}

	identical := true
	// ⟦gate B-6, 2026-09-06⟧ row accounting for the reconciliation line and
	// the vacuous-run refusal (see the header). presentBoth counts compare-set
	// projections found in BOTH dumps; totalA/totalB are their row totals.
	presentBoth, absentBoth := 0, 0
	var absentNames []string
	var totalA, totalB int
	for _, key := range compareSet {
		rowsA, okA := dumpA[key]
		rowsB, okB := dumpB[key]
		switch {
		case !okA && !okB:
			// Equal absence is equal — but a compare-set projection missing
			// from BOTH dumps means the deployed binaries predate it; warn
			// so a too-old pair of dumps cannot quietly verify itself.
			fmt.Fprintf(stderr, "warning: projection %q absent from both dumps (old binary?)\n", key)
			absentBoth++
			absentNames = append(absentNames, key)
		case okA != okB:
			side, other := "a", "b"
			if okB {
				side, other = "b", "a"
			}
			fmt.Fprintf(stdout, "DIFF %s: present in dump %s, MISSING from dump %s\n", key, side, other)
			identical = false
		default:
			presentBoth++
			totalA += len(rowsA)
			totalB += len(rowsB)
			if d := diffRows(key, rowsA, rowsB, stdout); d {
				identical = false
			}
		}
	}

	// W10-2(c)'s reconciliation, printed by the tool on every run so the
	// operator reads a row count instead of inferring one from file sizes.
	fmt.Fprintf(stdout, "compared: %d projections present in both dumps (rows a=%d b=%d); %d compare-set projections absent from both, not compared\n",
		presentBoth, totalA, totalB, absentBoth)

	if identical {
		// ⛔ D-85: equal emptiness is not agreement. Refuse before the word
		// IDENTICAL can be printed — it is the word the sheet has the
		// operator write down.
		if presentBoth == 0 || (totalA == 0 && totalB == 0) {
			fmt.Fprintln(stdout, "VACUOUS: 0 rows in every compared projection on both sides — nothing was compared; the §13.3 step-6 assert did NOT run")
			fmt.Fprintln(stderr, "dumpcompare: D-85 — two dumps of EMPTY stores compare equal by construction, so this run proves nothing.")
			fmt.Fprintln(stderr, "  Was `sitectl -dump` run with CWD /opt/sitectl (db_path in site.toml is RELATIVE) against the LIVE database,")
			fmt.Fprintln(stderr, "  and AFTER the sheet's two crossing tests? Re-take both dumps and run again. (exit 2: the assert never ran)")
			return 2
		}
		// ⟦G17, 2026-09-09 — James’s ruling: HARDEN THE VERDICT⟧ A compare-set
		// projection absent from BOTH dumps was never compared, so this run
		// cannot support the word IDENTICAL over the compare set. Until today
		// the tool warned on stderr, left `identical` true, and printed
		// "IDENTICAL: len(compareSet)" at exit 0 — a count LARGER than the
		// number it actually compared. [MEASURED 2026-09-09 at 48a85b5, with
		// `trade` and `trade_amendment` absent from both dumps: exit 0,
		// "compared: 22 …; 2 … absent from both", then
		// "IDENTICAL: 24 compared projections match".]
		//
		// ⚠ THE CRITERION PERMITS THIS AND DOES NOT REQUIRE IT. W10-2(c)’s
		// Narrowness clause: "This criterion does not require the tool to be
		// changed. DR-W10-4/W10-10 recommend it; if the plan owner declines,
		// the reconciliation stays a recorded operator step and the sheet
		// gains it at B-4." The plan owner did NOT decline. What the change
		// DOES invalidate is the criterion’s CONTROL, which reads "observe
		// exit 0" — that clause is now unreproducible and must be amended.
		// See reports/witness/G17-HARDEN-VERDICT.md.
		//
		// Exit 2, not 1: nothing DIFFERED. Part of the compare set was never
		// examined, which is this tool’s documented "the assert never ran"
		// code — the same judgement D-85’s VACUOUS refusal makes one clause up.
		//
		// ⛔ THIS CANNOT FIRE ON A LEGITIMATE EXCLUSION. alert_ack and
		// alert_ack_retraction (⟦was: "alert_ack, alert_ack_retraction and
		// weather_phase" — weather phases removed 2026-10-04⟧) are removed by
		// buildCompareSet BEFORE this loop is entered, so they land on the
		// `ignored` line and can never reach absentBoth. That separation is
		// structural, not a coincidence of the current list, and
		// TestG17TheHardFailureDoesNotFireOnTheLegitimateExclusions pins it.
		if absentBoth > 0 {
			fmt.Fprintf(stdout, "INCOMPLETE: %d of %d compare-set projections were absent from both dumps and never compared: %s\n",
				absentBoth, len(compareSet), strings.Join(absentNames, ", "))
			fmt.Fprintf(stderr, "dumpcompare: G17 — %d compare-set projection(s) are in NEITHER dump, so the §13.3 assert did not run over the whole compare set.\n", absentBoth)
			fmt.Fprintln(stderr, "  The binary that PRODUCED the dumps is older than the binary comparing them. Re-deploy both posts from the same build,")
			fmt.Fprintln(stderr, "  re-take both dumps, and run again. (exit 2: the assert never ran — the word IDENTICAL is deliberately NOT printed)")
			return 2
		}
		fmt.Fprintf(stdout, "IDENTICAL: %d compared projections match\n", len(compareSet))
		return 0
	}
	fmt.Fprintln(stdout, "DUMPS DIFFER (see DIFF lines above) — §13.3 step-6 assert FAILED")
	return 1
}

// diffRows compares one projection's row list in dump order (the dump is
// emitted under a deterministic ORDER BY, store/projection.go). Rows are
// re-marshalled through encoding/json, which sorts map keys — a canonical
// form, so formatting differences between the two HTTP bodies cannot
// produce a false diff and column-order differences cannot hide a real one.
func diffRows(key string, a, b []map[string]any, out io.Writer) (differs bool) {
	if len(a) != len(b) {
		fmt.Fprintf(out, "DIFF %s: %d rows in a, %d rows in b\n", key, len(a), len(b))
		differs = true
	}
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		ca, err := json.Marshal(a[i])
		if err != nil {
			fmt.Fprintf(out, "DIFF %s[%d]: cannot canonicalise a-side row: %v\n", key, i, err)
			return true
		}
		cb, err := json.Marshal(b[i])
		if err != nil {
			fmt.Fprintf(out, "DIFF %s[%d]: cannot canonicalise b-side row: %v\n", key, i, err)
			return true
		}
		if string(ca) != string(cb) {
			fmt.Fprintf(out, "DIFF %s[%d]:\n  a: %s\n  b: %s\n", key, i, ca, cb)
			// First differing row is enough to fail loudly; keep scanning
			// row counts but not every row body, to bound demo-day output.
			return true
		}
	}
	return differs
}

// load reads a saved dump file and decodes the top-level
// {projection: [rows...]} shape.
//
// A URL argument is REFUSED, not fetched. This tool no longer imports
// net/http at all, so the refusal is structural and not a runtime check that
// a later edit could drop — but the explicit message matters more than the
// mechanism: an operator with the old command in their notes gets told what
// to run instead, at the cutover, rather than a bare "no such file".
func load(arg string) (map[string][]map[string]any, error) {
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		return nil, fmt.Errorf("dump URLs no longer exist (D-56: the /debug/projection-dump route was removed). "+
			"Produce the file on the post instead: sitectl -config /opt/sitectl/site.toml -dump > dump.json; got %q", arg)
	}
	body, err := os.ReadFile(arg)
	if err != nil {
		return nil, err
	}
	var dump map[string][]map[string]any
	if err := json.Unmarshal(body, &dump); err != nil {
		// A file that is not a dump must be a loud parse error, never a
		// silent "empty dump equals empty dump" pass. This mattered most
		// when the input was an HTTP body — the C1 catch-all answers 200
		// with HTML for every unmatched URL, and two identical error pages
		// comparing equal would have been a green run that proved nothing.
		// It still matters on files: a truncated, empty or wrong-file
		// argument on both sides must fail, not agree.
		return nil, fmt.Errorf("not a projection dump (%w); first bytes: %.60s", err, body)
	}
	return dump, nil
}
