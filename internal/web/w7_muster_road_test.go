// w7_muster_road_test.go — W7-34, the "ON THE ROAD" block: THE FIRST
// CRITERION IN THIS PLAN THAT PINS IT AT ALL.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// ⚠⚠ NO LOCKED CRITERION PINS THIS BLOCK, and the lock re-verified that
// independently: `data-in-transit-vehicle`, `InTransitRow`, `InTransitFrom`
// and `InTransitCount` return ZERO hits in the plan and in
// `W5-POSTLOCK-AMENDMENTS.md`; the five other candidates were checked by hand
// and none covers it (`W6-8` is blind to grouping; `W6-9(b)` is the
// disclosure COPY; `W6-4` is scoped to W6's own diff; `W9a-3` fires on
// `trip_amended`; `W3-7` is the person boundary). The only checks in the area
// are sixteen NON-chunk-locked tests in `muster_in_transit_test.go`, NONE of
// which exercises two trips on one vehicle.
//
// ⭐ AND `W6-3`'s ORDERED LITERAL DOES NOT RED, WHICH IS CORRECT. `w6RosterNames`
// (`w6_print_render_test.go:51-74`) reads exactly two regions —
// `section[data-muster-section="on-site"]` and
// `div.summary[data-muster-section="folded"]` — and the in-transit block is in
// neither, with the exclusion written AT THE EXTRACTOR with its reason. So:
//
//	⚠⚠ A GREEN W6 SUITE IS NOT EVIDENCE THAT THE [[D-64]] GROUPING CHANGE WAS
//	CHECKED. `W6-3` is scoped out of this block BY DESIGN and goes green
//	whatever W7 does to it. THIS FILE IS THE ONLY CHECK THAT MOVES.
//
// Criteria carried here:
//
//   - W7-34(a) — THE RESOLVED IDENTITY, with the pre-join state as its OWN
//     POSITIVE CONTROL (two identities, proving the extractor is not matching
//     nothing — without it an after-only pin of "one identity" is satisfied by
//     an extractor returning the empty set).
//   - W7-34(b) — THE HEADLINE COUNT, the half [[D-64]] is actually about:
//     `({{.InTransitCount}} on {{len .InTransit}} vehicle…)` COUNTS ROWS AND
//     CALLS THEM VEHICLES. Certified: DISTINCT RESOLVED VEHICLE IDENTITIES.
//   - W7-34(c) — EVERY PERSON STILL APPEARS EXACTLY ONCE ([[D-64]]'s own
//     safety clause). ⚠ PLANT-DEPENDENT on its own face.
//   - W7-34(d) — the printed sheet and the board AGREE. ⚠⚠ A REGRESSION
//     GUARD, NOT A DISCRIMINATOR — see the annotation on the test.
//   - W7-34(e) — THE GRAMMAR AGREES WITH THE NUMBER BESIDE IT.
//
// ⚠⚠ WHAT W7 MUST **NOT** DO WITHOUT A FURTHER RULING: MERGE TWO IN-TRANSIT
// BLOCKS INTO ONE. That is the operation [[D-64]]'s safety clause warns about,
// it is the ONLY one that can drop a person, and it is NOT required by D-64's
// text. The block count is pinned UNCHANGED for exactly that reason.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL — it drives the real `/muster` and
// `/muster/print` handlers and reads the rendered markup — so it compiles and
// runs at e9cc5f5.
//
// ⟦RE-AUTHORED 2026-09-03 — post-lock §46, THE FIXTURE'S MECHANISM ONLY⟧
//
//	⚠⚠ WHAT CHANGED AND WHY, because a reader who diffs this file must not
//	mistake it for a criterion moving. `w7RoadWorld` SEEDED THE ROAD THROUGH
//	THE FREE-TEXT VEHICLE STRING DOOR — ⟦was: `func w7RoadWorld(t *testing.T,
//	vehicles [2]string)`, assigning `DispatchInput.VehicleEntered: vehicles[i]`
//	and `VehicleEntered: "TRK-CHECKIN"`⟧ — which is THE VERY DOOR
//	`internal/domain/w7_authoring_doors_test.go` forbids under `W7-4(d)(i)`
//	(mutant `(m7-4d)`, *a free-text vehicle string RESTORED as an authoring
//	input*). W7's builder found the two files CANNOT BOTH PASS and routed it
//	rather than picking a side; `reports/W5-POSTLOCK-AMENDMENTS.md` §46 RULED
//	THAT **THIS FIXTURE** IS THE ONE THAT IS WRONG — it encodes the pre-change
//	world — and that the door test, which encodes a LOCKED criterion, stands.
//
//	⚠ THE CONTRADICTION WAS MECHANICAL, NOT INTERPRETIVE: `reflect.Kind` of a
//	named string type is still `String`, so no third shape satisfied both.
//
//	The fixture now seeds via `VehicleID` PLUS a real vehicle RECORD authored
//	through `domain.CreateVehicle` — the ruled shape, in which *the typing
//	moment IS creation*. ⚠⚠ **NO ASSERTION IN THIS FILE CHANGED.** Every
//	check below still reads the same structural hooks, still certifies
//	DISTINCT RESOLVED VEHICLE IDENTITIES, and each was PROVED still able to
//	RED after the re-seed — see
//	reports/window-build/W7-FIXTURE-REAUTHOR.md for the planted defect and the
//	produced failure behind each one.
//
//	⚠ WHAT THE RE-SEED DOES NOT DO: it does not make `W7-4(d)(i)` pass. The
//	three authoring-door signatures still carry the string, because that is a
//	PRODUCTION change routed to the gate (`W7-build.md` §8.1) and no test
//	pass may make it. What this re-author buys is that the muster-road file no
//	longer BLOCKS it: nothing here depends on the string door any more.
//
// EXPECTED RED at e9cc5f5 (runtime):
//   - TestW7OnTheRoadHeadlineCountsDistinctVehiclesNotRows — the headline
//     reads "(6 on 2 vehicles)" for TWO OPEN RUNS OF ONE TRUCK, where the
//     certified pair is BLOCKS 2 / IDENTITIES 1.
//
// ⟦NO PRE-CHUNK RED⟧, named with the plant that would falsify each:
//   - TestW7OnTheRoadExtractorSeesTwoDistinctIdentities — (a)'s POSITIVE
//     CONTROL. Plant: an extractor scoped to a selector that matches nothing
//     returns the empty set and this test reds.
//   - TestW7OnTheRoadPersonSetIsPinned — (c). Plant: `(m7-3e)`, *union the
//     rows sharing a resolved vehicle*, applied ALONE.
//   - TestW7BoardAndPrintedSheetAgree — (d). ⚠ ON MOST BUILD SHAPES THIS
//     COMPARES ONE FUNCTION'S OUTPUT AGAINST ITSELF AND CANNOT RED. Its green
//     is NOT evidence that two-route agreement was engineered.
//
// KILLER MUTANTS (§5):
//
//	(m7-3b) INVERTED — the merge left UNRESOLVED on the in-transit render
//	(m7-34)  the headline counting ROWS and calling them vehicles
//	(m7-3e)  a merge that DROPS A PERSON from the in-transit set
//	(m7-34b) the resolution applied on ONE ROUTE AND NOT THE OTHER
//	(m7-34c) the headline number changed and the PLURALISATION not — "1 vehicles"
package web

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- extraction machinery (NO assertions) --------------------------------
//
// ⚠ Assertions are on STRUCTURAL HOOKS (`data-in-transit-vehicle`, the
// headline's own span) and never on a page-text scan: `DR-W7-13` forbids
// `strings.Contains` over a response body in EVERY W7 criterion, and §2.5
// measures `muster.html` at FIVE vehicle-rendering sites — so a body-contains
// assertion is satisfied by the page that has the bug.

var w7RoadVehicleAttr = regexp.MustCompile(`data-in-transit-vehicle="([^"]*)"`)

// w7HeadlineCount matches the ON THE ROAD headline's own numbers and its
// pluralisation, so (b) and (e) are read off ONE literal.
var w7Headline = regexp.MustCompile(`\((\d+) on (\d+) (vehicles?)\)`)

// w7RoadSection returns the in-transit section alone. W7 mints its own rather
// than calling `roadSection`: W7-X3 forbids another chunk's green counting
// toward W7's, and a helper shared across chunk boundaries is a dependency
// that can be deleted out from under it. ⚠ The stylesheet is stripped first —
// a class name in the one inlined stylesheet is not a rendered element (F19).
func w7RoadSection(t *testing.T, body string) string {
	t.Helper()
	page := styleRe.ReplaceAllString(body, "")
	at := strings.Index(page, `data-muster-section="in-transit"`)
	if at < 0 {
		t.Fatalf("W7-34 FIXTURE DEFECTIVE: the sheet has no ON THE ROAD section at all, so every assertion "+
			"below would be vacuous:\n%s", page)
	}
	end := strings.Index(page[at:], "</section>")
	if end < 0 {
		t.Fatal("W7-34 FIXTURE DEFECTIVE: the ON THE ROAD section is never closed")
	}
	return page[at : at+end]
}

// w7RoadIdentities returns the ORDERED list of `data-in-transit-vehicle`
// values — one per rendered block.
func w7RoadIdentities(t *testing.T, section string) []string {
	t.Helper()
	var out []string
	for _, m := range w7RoadVehicleAttr.FindAllStringSubmatch(section, -1) {
		out = append(out, m[1])
	}
	return out
}

func w7DistinctCount(vals []string) int {
	seen := map[string]bool{}
	for _, v := range vals {
		seen[v] = true
	}
	return len(seen)
}

// w7RoadPersons returns the SET of names rendered inside the in-transit
// blocks, sorted — [[D-64]]'s safety clause is about the SET, not the order.
var w7RoadName = regexp.MustCompile(`<li>([^<]+)</li>`)

func w7RoadPersons(t *testing.T, section string) []string {
	t.Helper()
	var out []string
	for _, m := range w7RoadName.FindAllStringSubmatch(section, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	sort.Strings(out)
	return out
}

// --- the fixture ---------------------------------------------------------

type w7Road struct {
	srv    *Server
	st     *store.Store
	riding [][]domain.Person
	// ⟦re-authored 2026-09-03⟧ vehicleIDs is the RECORD each of the two open
	// runs actually went out on, read back off the authored trip rather than
	// off the seed. It is what the fixture's own non-degeneracy precondition
	// is checked against: two runs of ONE truck are ONE id twice, two runs of
	// two trucks are TWO ids, and a fixture that cannot tell those apart
	// certifies nothing about a headline that counts identities.
	vehicleIDs [2]string
}

// w7RoadWorld puts TWO OPEN RUNS on the road from one post, EACH SEEDED BY
// VEHICLE **ID** AGAINST A REAL VEHICLE RECORD. `numbers` gives the NUMBER of
// the record each run goes out on, so one call builds either the TWO-IDENTITY
// state or the ONE-IDENTITY state.
//
// ⚠⚠ EQUAL NUMBERS MEAN **ONE RECORD, USED TWICE** — that is this fixture's
// whole non-degeneracy contract and it is CHECKED below, not asserted in a
// comment. Minting two records that happen to share a number would be the
// degenerate re-seed: `VehicleIdentity` is the resolved RECORD id, so two
// records reading "TRK-100" are TWO trucks and the one-identity state would
// silently become a second copy of the two-identity state. The headline
// assertion would then pass against a build that counts rows, and this file
// would certify nothing.
//
// ⚠ `numbers` IS NOT A FREE-TEXT AUTHORING INPUT AND MUST NOT BE READ AS ONE.
// It is handed to `domain.CreateVehicle` — THE CREATION EDGE, which under
// `DR-W7-4` is the one place a vehicle number is ever typed ("the typing
// moment IS creation"). Nothing below passes a vehicle string to a
// trip-authoring door; that is the entire point of the re-author.
//
// ⚠⚠ WHAT THIS FIXTURE IS AND IS NOT, stated because the criterion's own
// fixture is a TWO-STATE one (rendered BEFORE a join and AFTER it) and this
// one cannot be. `AmendVehicle` and the vehicle merge map do not exist at
// e9cc5f5, so the AFTER-A-JOIN state is UNPRODUCIBLE at lock time without
// naming an unbuilt symbol. What IS producible — and what exercises the same
// certified rule against the same mutant — is TWO OPEN RUNS OF ONE TRUCK:
// under the certified rule the headline says ONE identity while TWO blocks
// print, and under `(m7-34)` it says TWO. See
// reports/window-build/W7-tests.md §5 for what this leaves owed.
//
// ⚠ STATED SO THE RE-AUTHOR IS NOT MISREAD AS CLOSING THAT SHORTFALL: the
// merge map and `AmendVehicle` DO exist in the tree now, so the after-a-join
// state has become producible — but producing it would be a NEW assertion,
// not a re-seed of an existing one, and §46's ruling grants a MECHANISM
// change only. The shortfall at `W7-tests.md` §5 stands, unchanged and
// un-narrowed, and is still owed to a second test-writing pass.
func w7RoadWorld(t *testing.T, numbers [2]string) *w7Road {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/w7road.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road A", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
		},
	}
	srv := New(cfg, st)
	w := &w7Road{srv: srv, st: st}

	person := func(name string) domain.Person {
		p, err := domain.CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	crews := [][]string{
		{"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha"},
		{"Quinn, Aidan", "Rasmussen, Iver", "Silva, Marcos"},
	}
	var all []string
	for _, names := range crews {
		var crew []domain.Person
		for _, n := range names {
			p := person(n)
			crew = append(crew, p)
			all = append(all, p.PersonID)
		}
		w.riding = append(w.riding, crew)
	}
	// ⟦re-authored 2026-09-03 — post-lock §46⟧ THE VEHICLE RECORDS, minted
	// ONCE PER DISTINCT NUMBER and handed to every door below AS AN ID.
	//
	// ⚠ The memo map is what makes "two runs of one truck" mean ONE RECORD.
	// It is keyed on the number the caller asked for, NOT on the record's
	// composed number, because the caller's array is the fixture's statement
	// of intent and the composed number is an outcome of it — keying on the
	// outcome would make the fixture agree with itself by construction.
	byNumber := map[string]string{}
	vehicleID := func(number string) string {
		if id, ok := byNumber[number]; ok {
			return id
		}
		n := number
		v, err := domain.CreateVehicle(st, "site-a", 1, &n, nil, 0, "default")
		if err != nil {
			t.Fatalf("W7-34 FIXTURE DEFECTIVE: could not create the vehicle record %q that every "+
				"assertion below seeds through: %v", number, err)
		}
		byNumber[number] = v.VehicleID
		return v.VehicleID
	}

	// Everybody checks in first, so the on-site total is a real number the
	// dispatches move people OUT of — never a vacuous zero.
	//
	// ⚠ This door is `DispatchLocalCheckIn` and it takes the SAME
	// `DispatchInput`, so it was a SECOND seed through the string door and is
	// re-seeded by id for the same reason ⟦was: `VehicleEntered:
	// "TRK-CHECKIN"`⟧. Its trip is degenerate (no destination) and so is
	// never in the ON THE ROAD section — it is here for the on-site total
	// only, and its truck is deliberately a THIRD record so it can never be
	// confused with either run's.
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		VehicleID: vehicleID("TRK-CHECKIN"), DriverPersonID: all[0], OccupantPersonIDs: all[1:],
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	dest := "site-b"
	for i, crew := range w.riding {
		deadline := int64(90)
		in := domain.DispatchInput{
			VehicleID: vehicleID(numbers[i]), DriverPersonID: crew[0].PersonID,
			DestinationSite: &dest, DeadlineMinutes: &deadline,
		}
		for _, p := range crew[1:] {
			in.OccupantPersonIDs = append(in.OccupantPersonIDs, p.PersonID)
		}
		out, err := domain.Dispatch(st, "site-a", 1, in, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		// ⚠ READ BACK OFF THE AUTHORED TRIP, never off `in`. A dispatch door
		// that silently dropped the id — which is exactly the shape
		// `DR-W7-4` is about — would otherwise leave this fixture seeding
		// nothing while every count below still read whatever the fallback
		// produced.
		if out.VehicleID == "" {
			t.Fatalf("W7-34 FIXTURE DEFECTIVE: run %d was authored with NO vehicle id — the fixture seeds by id "+
				"and the trip did not keep it, so `VehicleIdentity` falls back to the recorded number and every "+
				"identity count below measures the OLD mechanism", i)
		}
		w.vehicleIDs[i] = out.VehicleID
	}

	// ⚠⚠ THE NON-DEGENERACY PRECONDITION, CHECKED RATHER THAN ASSUMED. Two
	// open runs of ONE truck must stay distinguishable from two runs of TWO
	// trucks at the SEED, or `TestW7OnTheRoadHeadlineCountsDistinctVehiclesNotRows`
	// certifies nothing: a fixture that minted two records for one truck
	// would make the one-identity state a second copy of the two-identity
	// state, and a build counting ROWS would pass it.
	wantDistinct := 2
	if numbers[0] == numbers[1] {
		wantDistinct = 1
	}
	if got := w7DistinctCount(w.vehicleIDs[:]); got != wantDistinct {
		t.Fatalf("W7-34 FIXTURE DEFECTIVE: numbers %v seeded %d DISTINCT vehicle record id(s) %v, want %d. "+
			"Equal numbers must mean ONE RECORD USED TWICE and different numbers must mean TWO RECORDS; a "+
			"fixture that cannot tell those two states apart cannot certify a headline that counts identities.",
			numbers, got, w.vehicleIDs, wantDistinct)
	}
	return w
}

func w7Page(t *testing.T, srv *Server, route string) string {
	t.Helper()
	return rawPage(t, srv, route)
}

// =========================================================================
// W7-34(a) — THE POSITIVE CONTROL: the extractor SEES two identities
// =========================================================================
//
// ⭐⭐ This is the criterion's own positive control, and it is why the
// two-state fixture exists at all: "Without it, an `after`-only pin of 'one
// identity' is satisfied by an extractor returning the EMPTY SET."
//
// ⟦NO PRE-CHUNK RED⟧ — it passes today and MUST. Its plant is any extractor
// change that stops matching `data-in-transit-vehicle`.
func TestW7OnTheRoadExtractorSeesTwoDistinctIdentities(t *testing.T) {
	w := w7RoadWorld(t, [2]string{"TRK-100", "TRK-200"})
	for _, route := range []string{"/muster", "/muster/print"} {
		sec := w7RoadSection(t, w7Page(t, w.srv, route))
		ids := w7RoadIdentities(t, sec)
		if len(ids) != 2 {
			t.Fatalf("W7-34(a) POSITIVE CONTROL FAILED on %s: the extractor found %d "+
				"`data-in-transit-vehicle` value(s) %v on a fixture with TWO open runs. An extractor that "+
				"matches nothing makes every 'one identity' assertion pass VACUOUSLY.", route, len(ids), ids)
		}
		if w7DistinctCount(ids) != 2 {
			t.Fatalf("W7-34(a) POSITIVE CONTROL FAILED on %s: two runs on DIFFERENT trucks rendered %d distinct "+
				"identities %v, want 2", route, w7DistinctCount(ids), ids)
		}
	}
}

// =========================================================================
// W7-34(b)+(e) — THE HEADLINE COUNT, and the grammar beside it
// =========================================================================
//
// Today [MEASURED — `muster.html:397`] the head reads
// `({{.InTransitCount}} on {{len .InTransit}} vehicle{{if ne (len .InTransit) 1}}s{{end}})`
// — IT COUNTS ROWS AND CALLS THEM VEHICLES.
//
// ⭐ CERTIFIED: DISTINCT RESOLVED VEHICLE IDENTITIES. On this fixture the
// headline is 1 WHILE THE BLOCK COUNT STAYS 2 — and that asymmetry, count 1 /
// blocks 2, IS THE WHOLE OBSERVABLE. Every rival build gives a different
// pair:
//
//	resolve-nothing        -> 2 / 2   (the pre-chunk tree: THIS red)
//	merge-the-blocks       -> 1 / 1   (the operation D-64's safety clause
//	                                   warns about — the only one that can
//	                                   drop a person)
//	resolve-but-count-rows -> 1 identity with 2 vehicles
//
// EXPECTED RED at e9cc5f5: "(6 on 2 vehicles)" where the certified headline
// is one vehicle over two blocks.
func TestW7OnTheRoadHeadlineCountsDistinctVehiclesNotRows(t *testing.T) {
	// ⚠ TWO OPEN RUNS OF ONE TRUCK. See w7RoadWorld's note: the criterion's
	// own fixture joins two vehicle RECORDS, which is unproducible at lock
	// time; this fixture exercises the same certified rule (DISTINCT RESOLVED
	// IDENTITIES) against the same mutant (ROWS COUNTED AS VEHICLES).
	w := w7RoadWorld(t, [2]string{"TRK-100", "TRK-100"})

	for _, route := range []string{"/muster", "/muster/print"} {
		sec := w7RoadSection(t, w7Page(t, w.srv, route))
		ids := w7RoadIdentities(t, sec)

		// The BLOCK COUNT stays 2. ⚠⚠ Pinned FIRST and separately, because
		// merging the blocks is the one operation that can drop a person and
		// it is NOT required by D-64's text.
		if len(ids) != 2 {
			t.Errorf("W7-34 on %s: the in-transit block count is %d, want 2. ⚠⚠ W7 MUST NOT MERGE TWO IN-TRANSIT "+
				"BLOCKS INTO ONE without a further ruling — that is the operation [[D-64]]'s safety clause warns "+
				"about, it is the ONLY one that can drop a person, and \"which group a person is listed under\" is "+
				"satisfied by the RESOLVED IDENTITY. (§10(a) Q6 is the further question, and the recommendation "+
				"there is NO.)", route, len(ids))
		}
		// The IDENTITY count is 1 — one truck, two open runs.
		if got := w7DistinctCount(ids); got != 1 {
			t.Errorf("W7-34(a)/(m7-3b): %s renders %d DISTINCT in-transit identities %v for two open runs of ONE "+
				"truck, want 1", route, got, ids)
		}

		// THE HEADLINE, read off ONE literal so (b) and (e) cannot disagree.
		m := w7Headline.FindStringSubmatch(sec)
		if m == nil {
			t.Fatalf("W7-34(b) FIXTURE DEFECTIVE: no `(N on M vehicle(s))` headline found in the ON THE ROAD "+
				"section on %s:\n%s", route, sec)
		}
		persons, _ := strconv.Atoi(m[1])
		vehicles, _ := strconv.Atoi(m[2])
		plural := m[3]

		if vehicles != 1 {
			t.Errorf("W7-34(b) (m7-34) on %s: the headline says %q — it counts ROWS and calls them VEHICLES. "+
				"CERTIFIED: DISTINCT RESOLVED VEHICLE IDENTITIES, so this fixture's certified pair is "+
				"IDENTITIES 1 / BLOCKS 2. ⚠ The current spelling OVER-REPORTS HOW MANY TRUCKS ARE ON THE ICE, "+
				"on the sheet an officer carries when the board cannot be reached.", route, m[0])
		}
		if persons != 6 {
			t.Errorf("W7-34(b) on %s: the headline's person count is %d, want 6 — `InTransitCount` sums "+
				"`len(run.Persons)` ACROSS ROWS (muster.go:284-287)", route, persons)
		}

		// (e) THE GRAMMAR AGREES WITH THE NUMBER BESIDE IT. ⚠ Today the
		// pluralisation is driven by `len .InTransit` — the ROW count — so a
		// build that changes the number and not the pluralisation prints
		// "1 vehicles" ON AN EVACUATION SHEET.
		wantPlural := "vehicles"
		if vehicles == 1 {
			wantPlural = "vehicle"
		}
		if plural != wantPlural {
			t.Errorf("W7-34(e) (m7-34c) on %s: the headline reads %q — %d and %q disagree. The pluralisation is "+
				"driven by `len .InTransit` (the ROW count) while (b) re-points the NUMBER at distinct resolved "+
				"identities, so the certified build prints \"1 vehicles\" unless the grammar is re-pointed too.",
				route, m[0], vehicles, plural)
		}
	}
}

// =========================================================================
// W7-34(c) — EVERY PERSON STILL APPEARS EXACTLY ONCE
// =========================================================================
//
// [[D-64]]'s own safety clause, made checkable. [[D-50]]'s safety clause is
// NOT weakened: its subject is "who is shown on-site", and joining two truck
// records changes WHICH GROUP a person is listed under, NEVER WHETHER THEY
// ARE LISTED.
//
// ⚠⚠ STATED HONESTLY, ON ITS FACE: under the MINIMAL reading NOTHING TOUCHES
// THE PERSON SET, so this clause is PLANT-DEPENDENT. Its value is as the
// guard against the MAXIMAL reading being implemented by accident, and its
// plant — `(m7-3e)`, applied ALONE — is *union the rows sharing a resolved
// vehicle*.
//
// ⚠ [MEASURED — `internal/web/muster.go:284-287`] `InTransitCount` sums
// `len(run.Persons)` ACROSS ROWS, so ANY implementation that unions rows MUST
// DEDUPE, and a dedupe is the one operation that can remove someone.
//
// ⟦NO PRE-CHUNK RED⟧ — it passes today and must.
func TestW7OnTheRoadPersonSetIsPinned(t *testing.T) {
	want := []string{
		"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha",
		"Quinn, Aidan", "Rasmussen, Iver", "Silva, Marcos",
	}
	sort.Strings(want)

	// The SAME six people, over BOTH identity states — so the pin is over the
	// person set and not over the grouping.
	for _, vehicles := range [][2]string{{"TRK-100", "TRK-200"}, {"TRK-100", "TRK-100"}} {
		w := w7RoadWorld(t, vehicles)
		for _, route := range []string{"/muster", "/muster/print"} {
			sec := w7RoadSection(t, w7Page(t, w.srv, route))
			got := w7RoadPersons(t, sec)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("W7-34(c) (m7-3e) on %s with vehicles %v:\n  got  %v\n  want %v\n"+
					"⚠ Every person still appears EXACTLY ONCE. [[D-64]] changes WHICH GROUP a person is listed "+
					"under, NEVER whether they are listed — and a criteria author must not read D-64 as licence "+
					"to let a merge remove anyone from the muster.", route, vehicles, got, want)
			}
			if len(got) != 6 {
				t.Errorf("W7-34(c) on %s: %d names rendered, want 6 — a dedupe keyed on NAME rather than on the "+
					"person id is `(m7-3e)` arm B, and `nameOf` falls back to the person id, so a name-space "+
					"collision is producible", route, len(got))
			}
		}
	}
}

// =========================================================================
// W7-34(d) — THE PRINTED SHEET AND THE BOARD AGREE
// =========================================================================
//
// ⚠⚠⚠ STATED HONESTLY, BECAUSE THE FALSIFIABILITY READ FOUND IT CONTINGENT:
// THIS CLAUSE IS A **REGRESSION GUARD, NOT A DISCRIMINATOR**, AND IT CAN RED
// AGAINST EXACTLY ONE BUILD SHAPE.
//
// [MEASURED — `muster.go:377` and `:534`] BOTH handlers call the SAME
// `s.inTransitRows()`, and [MEASURED — `muster.html:394-417`] the block is ONE
// unconditional `{{if .InTransit}}` section with NO print/live mode branch —
// `data-in-transit-vehicle` and the headline both read `.Vehicle` off the same
// rows. **So if the resolution lands INSIDE `inTransitRows()`,
// `domain.InTransitFrom` or the template, both routes get it BY CONSTRUCTION
// and this test compares ONE FUNCTION'S OUTPUT AGAINST ITSELF — it cannot
// red.**
//
// ⭐ It reds against the one shape that CAN diverge, and that shape is
// plausible rather than theoretical: THE RESOLUTION APPLIED IN THE TWO HANDLER
// BODIES, AFTER THE SHARED CALL. The handlers already assemble and pass the
// company and workgroup grouping maps (`muster.go:156`), so a vehicle merge
// map assembled the same way and passed at ONE call site and not the other
// compiles and ships. That is `(m7-34b)` exactly.
//
// ⚠⚠ THE CONSEQUENCE A GATE MUST NOT MISREAD: **A GREEN HERE IS NOT EVIDENCE
// THAT TWO-ROUTE AGREEMENT WAS ENGINEERED — on most build shapes it is
// evidence that one code path was used twice.** Its value is that it fails
// loudly if a builder ever splits the path, on the artefact an officer carries
// when the board cannot be reached.
//
// ⚠⚠ THIS TEST DOES NOT PIN WHERE THE RESOLUTION LANDS. That is a build
// choice; the lock DELIBERATELY did not pin it, and neither does this file.
func TestW7BoardAndPrintedSheetAgree(t *testing.T) {
	for _, vehicles := range [][2]string{{"TRK-100", "TRK-200"}, {"TRK-100", "TRK-100"}} {
		w := w7RoadWorld(t, vehicles)

		live := w7RoadSection(t, w7Page(t, w.srv, "/muster"))
		print := w7RoadSection(t, w7Page(t, w.srv, "/muster/print"))

		liveIDs, printIDs := w7RoadIdentities(t, live), w7RoadIdentities(t, print)
		if strings.Join(liveIDs, "|") != strings.Join(printIDs, "|") {
			t.Errorf("W7-34(d) (m7-34b) with vehicles %v: the IDENTITY LISTS differ.\n  /muster       %v\n"+
				"  /muster/print %v\n⚠ The board and the sheet would name DIFFERENT TRUCKS for the same open run "+
				"AT THE SAME INSTANT, on the artefact an officer carries when the board cannot be reached.",
				vehicles, liveIDs, printIDs)
		}
		if len(liveIDs) != len(printIDs) {
			t.Errorf("W7-34(d) (m7-34b) with vehicles %v: block counts differ, %d vs %d",
				vehicles, len(liveIDs), len(printIDs))
		}
		livePeople, printPeople := w7RoadPersons(t, live), w7RoadPersons(t, print)
		if strings.Join(livePeople, "|") != strings.Join(printPeople, "|") {
			t.Errorf("W7-34(d) (m7-34b) with vehicles %v: the PERSON SETS differ.\n  /muster       %v\n"+
				"  /muster/print %v", vehicles, livePeople, printPeople)
		}
		lh := w7Headline.FindStringSubmatch(live)
		ph := w7Headline.FindStringSubmatch(print)
		if lh == nil || ph == nil {
			t.Fatalf("W7-34(d) FIXTURE DEFECTIVE: a headline was missing (live=%v print=%v)", lh != nil, ph != nil)
		}
		if lh[0] != ph[0] {
			t.Errorf("W7-34(d) (m7-34b) with vehicles %v: the HEADLINE COUNTS differ — %q vs %q",
				vehicles, lh[0], ph[0])
		}
	}
}
