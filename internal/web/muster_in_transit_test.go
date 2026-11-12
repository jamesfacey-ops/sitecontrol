// muster_in_transit_test.go — D-6's fourth muster population: ON THE ROAD.
//
// THE DEFECT, measured before any of this existed. On a live two-site
// instance: six people checked in at site-a, three of them dispatched to
// site-b on TRK-100. site-a's /muster and /muster/print both read 3 on site
// / 0 away / 0 not tracked. site-b's both read 0 / 0 / 0. The three people
// in transit were named on NEITHER post's sheet, on screen or on paper,
// while the store held their vehicle, driver, manifest, destination and a
// running deadline at the far end. Under D-6's 100% accountability rule —
// an OSHA rule applied on the Slope, enforced for insurance reasons, with no
// regulation number — that is an accountability gap.
//
// THE OWNERSHIP DECISION THESE TESTS PIN. The ORIGIN post carries them, not
// the destination that is expecting them. Both halves are asserted, and the
// second half is the one that matters: TestOnlyTheDispatchingPostNamesACrewOnTheRoad
// proves that summing every post's sheet counts each in-transit person
// EXACTLY ONCE — never twice (trip.OriginSite is single-valued and authored
// structurally as env.SiteID, so only one site can match) and never zero
// (the matching site is the one that authored the event, so it cannot be
// missing it, link up or down). The reasoning is in domain/in_transit.go.
//
// Assertions here are on structural hooks (data-muster-section,
// data-tally-count, data-in-transit-vehicle) or on the parsed stylesheet,
// never on a page-text scan, and the stylesheet is stripped before any
// markup assertion — a class name in the one inlined stylesheet is not a
// rendered element (F19).
package web

import (
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- fixtures ---------------------------------------------------------

// roadWorld is one post that has put a crew on the road: three people
// checked in at site-a, three others checked in and then dispatched to
// site-b on TRK-100 with a 90-minute allowance. Deliberately the same shape
// as the live instance the defect was measured on, so a green run here and
// a green run against a real server are talking about the same scenario.
type roadWorld struct {
	srv     *Server
	st      *store.Store
	trip    domain.Trip
	staying []domain.Person // still on site at site-a
	riding  []domain.Person // on TRK-100, driver first
}

func newRoadWorld(t *testing.T) *roadWorld {
	t.Helper()
	srv, st := newMusterTestServer(t, 0)
	// A configured controlled destination, so the sheet can print the post
	// NAME a human says out loud instead of the site id every row keys off.
	srv.Cfg.Destinations = []config.Destination{
		{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
		// An uncontrolled pad too, so the NOT ON SITE section exists on the
		// rendered sheet and the reading-order test below has a real fourth
		// landmark to place this section against rather than an absent one.
		{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
	}

	person := func(name string) domain.Person {
		p, err := domain.CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	w := &roadWorld{srv: srv, st: st}
	for _, n := range []string{"Quinn, Aidan", "Rasmussen, Iver", "Silva, Marcos"} {
		w.staying = append(w.staying, person(n))
	}
	for _, n := range []string{"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha"} {
		w.riding = append(w.riding, person(n))
	}
	// Everybody arrives first, so the on-site total is a real number the
	// dispatch has to move people OUT of — not a vacuous zero.
	var all []string
	for _, p := range append(append([]domain.Person{}, w.staying...), w.riding...) {
		all = append(all, p.PersonID)
	}
	checkIn(t, st, "site-a", all[0], all[1:])

	w.trip = dispatchToPad(t, st, "TRK-100", w.riding, 90)
	return w
}

// dispatchToPad authors one open controlled run from site-a to site-b.
// riding[0] drives; every rider is an occupant (§7).
// ⟦was: a trailing `override string` — a non-empty reason dispatched the run
// through a blocking-phase PhaseGate; weather phases removed, James
// 2026-10-04⟧
func dispatchToPad(t *testing.T, st *store.Store, vehicle string, riding []domain.Person, deadline int64) domain.Trip {
	t.Helper()
	dest := "site-b"
	in := domain.DispatchInput{
		// ⚠⚠⚠ ⟦ROUTED BACK A SECOND TIME — §65 condition (iii), 2026-09-03⟧ §65 ruled a PRODUCTION
		// DISPLAY FIX here — "a display surface must render the derived NUMBER, never the record
		// id" — and set the condition: "if the measured design is not as I describe, ROUTE IT
		// BACK AGAIN." ⚠ IT IS NOT AS DESCRIBED, in two ways, and the second one is decisive.
		//
		// 1. THERE IS NO DISPLAY DEFECT. [MEASURED by rendering, not by reading] with a real
		//    record the sheet ALREADY prints the number, in the heading operators read:
		//        <h3 class="road-head"><code>TRK-100</code> &#8594; Pad Site Control …
		//    `in_transit.go:299-301` already does `display = *v.Number`. What carries the record
		//    id is this ONE hook, `muster.html:418` -> `{{.VehicleIdentity}}`.
		//
		// 2. ⚠⚠ THE HOOK IS AN IDENTITY BY W7's LOCKED DESIGN, AND A SECOND LOCKED TEST READS IT
		//    THAT WAY. `w7_muster_road_test.go:127` extracts it with
		//    `regexp.MustCompile('data-in-transit-vehicle="([^"]*)"')` and counts DISTINCT values
		//    (`:392`, `:442`). Its whole non-degeneracy contract is stated at `:205-212`: "two
		//    records reading 'TRK-100' are TWO trucks and the one-identity state would silently
		//    become a second copy of the two-identity state … this file would certify nothing."
		//    ⛔ Rendering the NUMBER in this attribute would leave BOTH of that file's tests
		//    GREEN while removing the identity semantics they are built on — the uninspectable
		//    green §60 bars in terms.
		//
		// ⭐ SO THIS IS NOT A FIXTURE PROBLEM AND NOT A PRODUCTION BUG: it is TWO LOCKED TESTS
		// WANTING ONE ATTRIBUTE TO CARRY TWO DIFFERENT THINGS. `muster_in_transit_test.go` pins
		// it to a LABEL ("TRK-100", a pre-W7 expectation from when identity fell back to the
		// recorded number); `w7_muster_road_test.go` requires it to be an IDENTITY. §46 is the
		// precedent for exactly this class — "W7's two contradictory tests: the MUSTER-ROAD
		// FIXTURE is the one that is wrong" — and which one gives way is the PLAN OWNER's.
		//
		// ⚠ A CORRECTION TO THIS PASS'S OWN EARLIER REPORT, recorded because it was wrong in the
		// record: the first route-back said changing the hook would break the distinct-vehicle
		// COUNT. It would not. [MEASURED — `muster.go:298-306`] the count is computed in Go from
		// the `VehicleIdentity` STRUCT FIELD before rendering, and the HTML attribute has NO
		// non-test consumer at all. The count was never the obstacle; the second locked test is.
		//
		// ⚠ ⟦ROUTED BACK — §62, 2026-09-03⟧ THIS FILE IS ONE OF THE FOUR §62 bar 3 LEAVES
		// UN-PRE-CLASSIFIED, AND ON ITS OWN EVIDENCE IT FITS NONE OF R1/R2/R3. Bar 3: "if it
		// fits none, ROUTE IT BACK rather than forcing the nearest fit." Not R1 (no guard
		// moved — nothing here is a refusal), not R2 (nothing became unconstructible), not R3
		// (no event count, no measured window — this reads rendered markup).
		//
		// ⚠⚠ AND BOTH AVAILABLE REPAIRS ARE WRONG, IN OPPOSITE DIRECTIONS. That is why it is
		// routed rather than chosen between. [MEASURED — `domain/in_transit.go:292-325`]
		//   (a) SEED A REAL RECORD: `identity := ResolveVehicleID(vehicleMerge, t.VehicleID)`
		//       becomes the minted `site-a-vehicle-<hex>`, so `data-in-transit-vehicle` renders
		//       that instead of `TRK-100` — reddening the literal this file pins at `:167` and
		//       again at `:428`. This is the note below, and it is correct.
		//   (b) PASS A BARE ID: the pinned literals SURVIVE, because with no record to resolve
		//       `identity = display`. ⛔ But `display := derefString(t.VehicleEntered)` is then
		//       "" — `vehicleNumberSnapshot` answers a miss with NIL, not an error ("A MISS IS
		//       NOT A REFUSAL", tripauthor.go:456) — so THE SHEET RENDERS NO VEHICLE AT ALL
		//       while every check in this file goes green. That is the green-going substitution
		//       §62 and the standing rules bar in terms, and it is the more tempting of the two
		//       precisely because it compiles and passes.
		// ⚠ Choosing between a pinned identity and a rendered vehicle is a criteria question,
		// not a fixture repair. PLAN OWNER's.
		//
		// ⚠ ⟦ROUTED — §47 condition 3, 2026-09-03⟧ NOT RE-SEEDED. `in_transit.go`'s identity
		// fallback `identity = display` fires ONLY while `vehicle_id` is empty. A real record
		// fills it, so `data-in-transit-vehicle` renders `site-a-vehicle-<hex>` instead of
		// `TRK-100` — and this file pins that hook as a LITERAL, twice, below. Production's own
		// comment names the hazard in terms: "The normalised form would change the value of a
		// hook that locked muster tests pin as a literal." The display half survives; the
		// IDENTITY half does not, so the fixture's MEANING changed. PLAN OWNER's.
		// See reports/window-build/RESEED-164.md §3.8.
		// ⟦LOCK — §71(b), 2026-09-03⟧ RE-SEEDED BY ID. ⚠ THE MEMOISATION IS THE FIXTURE'S
		// MEANING HERE, not an optimisation: `w7SeedVehicleID` keys on the NUMBER, so two calls
		// with the same `vehicle` string name ONE record used twice — "two runs of one truck" —
		// and two different strings name two records. That is exactly the discrimination the
		// identity hook is asserted on below.
		// ⟦was: "VehicleEntered: vehicle, DriverPersonID: riding[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", vehicle), DriverPersonID: riding[0].PersonID,
		DestinationSite: &dest, DeadlineMinutes: &deadline,
	}
	for _, p := range riding[1:] {
		in.OccupantPersonIDs = append(in.OccupantPersonIDs, p.PersonID)
	}
	trip, err := domain.Dispatch(st, "site-a", 1, in, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

// ⟦LOCK — §71(b), 2026-09-03⟧ musterRoadHookAttr / musterRoadHookValues read the identity hook.
//
// ⚠ MINTED HERE RATHER THAN SHARED WITH `w7_muster_road_test.go`, WHICH HAS AN IDENTICAL
// EXTRACTOR. That duplication is deliberate and its reason is stated in that file: `W7-X3`
// forbids another chunk's green counting toward W7's, and a helper shared across a chunk
// boundary is exactly that dependency. Two extractors that agree are the point.
var musterRoadHookAttr = regexp.MustCompile(`data-in-transit-vehicle="([^"]*)"`)

func musterRoadHookValues(t *testing.T, sec string) []string {
	t.Helper()
	var out []string
	for _, m := range musterRoadHookAttr.FindAllStringSubmatch(sec, -1) {
		out = append(out, m[1])
	}
	return out
}

// musterExtraCrew creates people who are not already on a run, so a second
// dispatch has somebody to carry.
func musterExtraCrew(t *testing.T, st *store.Store, names ...string) []domain.Person {
	t.Helper()
	var out []domain.Person
	for _, n := range names {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestTheInTransitHookIsAnIdentityNotALabel — ⟦LOCK — §71(b), 2026-09-03⟧
//
// ⭐ THE HOOK IS PINNED BY DISCRIMINATION, BECAUSE IT CANNOT BE PINNED BY VALUE. After
// `W7-34(a)` `data-in-transit-vehicle` carries the RESOLVED RECORD ID, and a record id comes
// from `crypto/rand` — so no test can name it in advance. What IS stable is the PROPERTY the
// hook exists to carry: same truck, same value; different trucks, different values.
//
// ⚠ WHY THIS IS NOT THE SAME CHECK AS THE LABEL PIN, and why both are needed. The label
// (`<code>TRK-100</code>`) is what a human reads and is pinned by value elsewhere in this file.
// If the hook were ALSO the label, then two DIFFERENT vehicle records that happen to share a
// number would render the same hook — and the sheet's own headline, which counts DISTINCT
// identities (`muster.go:298-306`), would say ONE truck while TWO blocks printed. That is the
// exact ambiguity `in_transit.go:325-328` says item 9 removes.
//
// ⚠ PRODUCED, not argued: with two DISTINCT records both numbered "TRK-100", arm 1 below FAILS
// — the two hook values differ, because they are ids. A hook that carried the NUMBER would pass
// arm 1 on that fixture and this test would be pinning nothing. See
// reports/window-build/S62-ROUTES.md §17.
func TestTheInTransitHookIsAnIdentityNotALabel(t *testing.T) {
	t.Run("two runs of ONE truck carry the SAME hook value", func(t *testing.T) {
		w := newRoadWorld(t) // run 1, on TRK-100
		// The SAME number seeds the SAME record (w7SeedVehicleID memoises on it),
		// so this is one truck making two runs — not two trucks.
		dispatchToPad(t, w.st, "TRK-100", musterExtraCrew(t, w.st, "Tanaka, Rin", "Ualdo, Bea"), 45)

		ids := musterRoadHookValues(t, roadSection(t, rawPage(t, w.srv, "/muster/print")))
		if len(ids) != 2 {
			t.Fatalf("CONTROL FAILED: the extractor found %d hook value(s) %v on a sheet with TWO open runs. "+
				"An extractor that matches nothing makes every identity assertion below pass VACUOUSLY", len(ids), ids)
		}
		if ids[0] != ids[1] {
			t.Fatalf("two runs of ONE truck rendered DIFFERENT identities %v — the headline counts distinct "+
				"identities, so this sheet would report two trucks on the road when one truck made two runs", ids)
		}
	})

	t.Run("two runs of DIFFERENT trucks carry DIFFERENT hook values", func(t *testing.T) {
		w := newRoadWorld(t) // run 1, on TRK-100
		dispatchToPad(t, w.st, "SNOWCAT 2", musterExtraCrew(t, w.st, "Tanaka, Rin", "Ualdo, Bea"), 45)

		ids := musterRoadHookValues(t, roadSection(t, rawPage(t, w.srv, "/muster/print")))
		if len(ids) != 2 {
			t.Fatalf("CONTROL FAILED: the extractor found %d hook value(s) %v on a sheet with TWO open runs", len(ids), ids)
		}
		if ids[0] == ids[1] {
			t.Fatalf("two runs on DIFFERENT trucks rendered the SAME identity %q — the headline would report "+
				"ONE truck on the road while TWO blocks print, and an evacuation would be told to look for "+
				"one vehicle", ids[0])
		}
	})
}

// roadSection returns the markup of the ON THE ROAD section alone, with the
// stylesheet already stripped. Scoping matters: a page-wide grep would let
// a word from the tally block or from another section stand in for
// something this section is supposed to say.
func roadSection(t *testing.T, body string) string {
	t.Helper()
	page := styleRe.ReplaceAllString(body, "")
	at := strings.Index(page, `data-muster-section="in-transit"`)
	if at < 0 {
		t.Fatalf("the sheet has no ON THE ROAD section at all:\n%s", page)
	}
	end := strings.Index(page[at:], "</section>")
	if end < 0 {
		t.Fatal("the ON THE ROAD section is never closed")
	}
	return page[at : at+end]
}

func hasRoadSection(body string) bool {
	return strings.Contains(styleRe.ReplaceAllString(body, ""), `data-muster-section="in-transit"`)
}

// --- the gap itself ---------------------------------------------------

// THE REGRESSION TEST FOR THE WHOLE FEATURE. Every field James named —
// vehicle, occupants, driver, destination, time due — asserted on BOTH
// routes, because the screen and the paper are separate render paths and
// this project has twice shipped a print claim that could not fail.
//
// "Time due" is asserted as the ALLOWANCE, not as a clock time, and that is
// a deliberate limit rather than a shortcut: the origin holds no time basis
// for its own outbound trips (deadline_capture rows are written only where
// destination_site is this site) and the replication envelope carries no
// wall clock by design (§9.3). The allowance is authored data and true
// everywhere; a computed "due at HH:MM" here would be invented. See
// TestTheRoadSectionSaysWhereTheClockActuallyIs.
func TestTheMusterNamesThePeopleThisPostPutOnTheRoad(t *testing.T) {
	w := newRoadWorld(t)

	for _, route := range []string{"/muster", "/muster/print"} {
		sec := roadSection(t, rawPage(t, w.srv, route))
		for _, want := range []string{
			// ⟦LOCK — §71(b), 2026-09-03⟧ THE LABEL IS PINNED BY VALUE; THE IDENTITY IS NOT.
			// ⟦was: "`data-in-transit-vehicle=\"TRK-100\"`, // the vehicle"⟧
			// ⚠ The hook carries the RESOLVED RECORD ID after `W7-34(a)`, and a record id comes
			// from crypto/rand (§57 c1) — it CANNOT be pinned by value by anyone. What a human
			// reads on the sheet is the label, `<code>{{.Vehicle}}</code>` (muster.html:420),
			// and that is deterministic. So this pin moves to the label and the identity is
			// asserted BY DISCRIMINATION in TestTheInTransitHookIsAnIdentityNotALabel below.
			// ⚠ THIS IS NOT A WEAKENING: the old pin passed only because identity USED to fall
			// back to the recorded number when no record existed. It was pinning the fallback.
			"<code>TRK-100</code>", // the vehicle, by the label an operator actually reads
			"Pad Site Control",     // the destination, by post name
			"Nadeau, Pierre",       // the driver, named as driver...
			"Ohara, Wendy",         // ...and every occupant
			"Petrov, Sasha",
			"90 minutes", // the journey allowance recorded at dispatch
		} {
			if !strings.Contains(sec, want) {
				t.Errorf("%s: the ON THE ROAD section does not carry %q:\n%s", route, want, sec)
			}
		}
		// Singular and plural both correct. MEASURED on a real printed
		// sheet, 2026-08-21: a one-minute allowance printed "1 minutes",
		// because DeadlineMinutes is a POINTER and a template cannot
		// compare it to 1. A muster read aloud during an evacuation may not
		// carry that kind of tell — it is the first thing that makes a
		// reader distrust the rest of the sheet.
		if strings.Contains(sec, "1 minutes") {
			t.Errorf("%s: the allowance prints an ungrammatical plural:\n%s", route, sec)
		}

		// The driver is named AS the driver, not merely present in the
		// manifest — D-4's flag exists so an incident report can say who
		// was at the wheel, and a name in a list does not answer that.
		if !strings.Contains(sec, "Driver:") {
			t.Errorf("%s: the section names nobody as the driver:\n%s", route, sec)
		}
		// Nobody who stayed behind leaks into this section.
		for _, p := range w.staying {
			if strings.Contains(sec, p.Name) {
				t.Errorf("%s: %s is on site but appears in the ON THE ROAD section", route, p.Name)
			}
		}
	}
}

// They are NOT in the on-site total, and the sheet states their number
// separately — the headcount block's whole contract is that it names every
// category deliberately excluded from the big number.
func TestPeopleOnTheRoadAreCountedSeparatelyFromTheOnSiteTotal(t *testing.T) {
	w := newRoadWorld(t)

	for _, route := range []string{"/muster", "/muster/print"} {
		page := styleRe.ReplaceAllString(rawPage(t, w.srv, route), "")
		if !strings.Contains(page, `data-tally-count="on-site">3<`) {
			t.Errorf("%s: on-site total is not 3 — the three riders are still being counted as present:\n%s", route, page)
		}
		if !strings.Contains(page, `data-tally-count="in-transit">3<`) {
			t.Errorf("%s: the headcount does not state 3 people on the road:\n%s", route, page)
		}
		if !strings.Contains(page, `data-tally-row="in-transit"`) {
			t.Errorf("%s: the headcount block has no on-the-road row at all", route)
		}
	}
	// The on-site roster itself must not list them either — the tally is
	// derived from Board.Total, so asserting the number alone would pass
	// while a rider's name sat under a company heading.
	roster := rawPage(t, w.srv, "/muster")
	on := strings.Index(roster, `data-muster-section="on-site"`)
	road := strings.Index(roster, `data-muster-section="in-transit"`)
	if on < 0 || road < 0 || road < on {
		t.Fatalf("expected the on-site section then the road section; got %d and %d", on, road)
	}
	for _, p := range w.riding {
		if strings.Contains(roster[on:road], p.Name) {
			t.Errorf("%s is on the road but is listed inside the ON SITE roster", p.Name)
		}
	}
}

// --- ownership, and the double-count argument -------------------------

// THE OWNERSHIP DECISION, ASSERTED AS ARITHMETIC. The same trip is present
// in both posts' stores (as it is after replication). The ORIGIN names the
// crew; the DESTINATION does not. Summed across the two sheets each person
// therefore appears EXACTLY ONCE.
//
// This is the test that would go red if somebody "helpfully" made the
// destination render its inbound expectations here too: that reads as more
// accountability and is less, because an evacuation coordinator adding two
// sheets together would count the same three people twice and search for
// six. It would ALSO go red if ownership were moved to the destination,
// because the origin's sheet would then be the empty one — and the origin
// is the post whose own stream always holds the record, link or no link.
func TestOnlyTheDispatchingPostNamesACrewOnTheRoad(t *testing.T) {
	w := newRoadWorld(t)

	// The destination post, holding the SAME trip — the state site-b is in
	// once the dispatch has replicated.
	peer, peerStore := newMusterTestServer(t, 0)
	peer.Cfg.SiteID = "site-b"
	replicateTripInto(t, w.st, peerStore, w.trip.TripID)

	originSheet := rawPage(t, w.srv, "/muster/print")
	peerSheet := rawPage(t, peer, "/muster/print")

	// Precondition: site-b really does hold the trip, or "site-b does not
	// show them" is true because site-b knows nothing at all.
	rows, err := domain.InTransitFrom(peerStore, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("precondition: the peer store does not hold the open trip (%d rows) — this test would pass vacuously", len(rows))
	}

	if !hasRoadSection(originSheet) {
		t.Fatal("the origin post — which dispatched them — carries no ON THE ROAD section")
	}
	if hasRoadSection(peerSheet) {
		t.Fatalf("the DESTINATION post also names the crew on the road; summing the two sheets double-counts them:\n%s", peerSheet)
	}
	if !strings.Contains(peerSheet, `data-tally-count="in-transit">0<`) {
		t.Errorf("the destination's headcount does not state zero on the road — it must say it checked the category, not stay silent")
	}

	// The arithmetic, stated as arithmetic: exactly one appearance each,
	// across every sheet the operation prints.
	for _, p := range w.riding {
		got := strings.Count(styleRe.ReplaceAllString(originSheet, ""), p.Name) +
			strings.Count(styleRe.ReplaceAllString(peerSheet, ""), p.Name)
		// The driver is named twice on the origin sheet by design (once as
		// "Driver:", once in the manifest), so the driver's floor is 2.
		want := 1
		if p.PersonID == w.riding[0].PersonID {
			want = 2
		}
		if got != want {
			t.Errorf("%s appears %d times across both posts' sheets, want %d — a person must be accounted for exactly once", p.Name, got, want)
		}
	}
}

// replicateTripInto copies one trip's dispatch envelope from src to dst,
// exactly as the replication engine would. Envelopes are re-applied through
// the store's own ingest path, so the destination's projections fold it the
// same way the origin's did.
func replicateTripInto(t *testing.T, src, dst *store.Store, tripID string) {
	t.Helper()
	envs, err := src.Events("site-a", 1, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	// The whole contiguous prefix up to and including the dispatch, exactly
	// as the engine delivers it: the store refuses a gap in a stream (§8.3),
	// and cherry-picking one envelope would be testing a delivery this
	// system never performs.
	seen := false
	for _, e := range envs {
		if _, err := dst.Apply(e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(e.Payload), tripID) {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("no envelope in the origin store mentions trip %s", tripID)
	}
}

// --- emptying: arrival and cancel -------------------------------------

// A run leaves the section when its arrival is recorded. Asserted with a
// BEFORE and an AFTER on the same server, so "it is gone" cannot be true
// because it was never there.
func TestTheRoadSectionEmptiesWhenTheRunArrives(t *testing.T) {
	w := newRoadWorld(t)
	if !hasRoadSection(rawPage(t, w.srv, "/muster/print")) {
		t.Fatal("precondition: the crew is not on the road before the arrival")
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := rawPage(t, w.srv, "/muster/print")
	if hasRoadSection(after) {
		t.Fatalf("the crew is still listed on the road after arriving:\n%s", after)
	}
	if !strings.Contains(after, `data-tally-count="in-transit">0<`) {
		t.Error("the headcount still counts them on the road after arrival")
	}
}

// A CANCEL must not strand anyone in this section. It also must not be read
// as putting them anywhere: TripCancelled never changes a visit state, so
// the occupants stay checked out of the origin and land in no muster
// category at all — a REAL, PRE-EXISTING hole (NotTracked excludes
// cancelled trips by its own contract) that is reported to James rather
// than silently absorbed here. This test pins the half this change owns:
// the section empties, and it does not keep claiming a crew is en route to
// a destination the record says they are not going to.
//
// ⟦D-78, 2026-09-05⟧ THE HOLE THIS TEST NAMED IS NOW UNREACHABLE FROM ANY
// SCREEN. D-73 ruled that no trip is cancelled and D-78 closed the route;
// the cancel below is authored DIRECTLY through the domain, as every test
// pinning the retained state does, so what this pins is the fold's answer
// to already-replicated history. The officer-facing half — three people on
// the road, the old POST attempted, all three still on the sheet — is
// TestD78ThreeOnTheRoadSurviveTheOldCancelPost.
func TestACancelledRunStrandsNobodyOnTheRoad(t *testing.T) {
	w := newRoadWorld(t)
	if !hasRoadSection(rawPage(t, w.srv, "/muster/print")) {
		t.Fatal("precondition: the crew is not on the road before the cancel")
	}
	if err := domain.CancelTrip(w.st, "site-a", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := rawPage(t, w.srv, "/muster/print")
	if hasRoadSection(after) {
		t.Fatalf("a cancelled run still shows its crew as on the road:\n%s", after)
	}
	if !strings.Contains(after, `data-tally-count="in-transit">0<`) {
		t.Error("the headcount still counts a cancelled run's occupants on the road")
	}
}

// The within-sheet double-count guard. A rider who turns back and checks in
// again at the origin while the run is still open is ON SITE — and must not
// also be named as on the road on the very same sheet. The predicate is
// domain's shared sightedSince, the same one OutAtUncontrolled and
// NotTracked use, so the four categories cannot disagree about what counts
// as a recorded return.
func TestARiderBackAtTheOriginIsNotAlsoNamedOnTheRoad(t *testing.T) {
	w := newRoadWorld(t)
	turnedBack := w.riding[1] // Ohara

	// Ohara walks back in at site-a: a fresh local check-in, authored AFTER
	// the dispatch on site-a's own stream.
	checkIn(t, w.st, "site-a", turnedBack.PersonID, nil)

	page := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster/print"), "")
	sec := roadSection(t, rawPage(t, w.srv, "/muster/print"))
	if strings.Contains(sec, turnedBack.Name) {
		t.Errorf("%s is back on site and is STILL named on the road — one person, two places, on one sheet:\n%s", turnedBack.Name, sec)
	}
	if !strings.Contains(page, `data-tally-count="in-transit">2<`) {
		t.Errorf("the on-the-road count did not drop to 2 when a rider came back:\n%s", page)
	}
	// ...and they really are counted as on site, so this is a MOVE between
	// categories and not a disappearance.
	if !strings.Contains(page, `data-tally-count="on-site">4<`) {
		t.Errorf("the on-site total is not 4 after a rider returned — they have fallen off the sheet entirely:\n%s", page)
	}
}

// --- counting ---------------------------------------------------------

// len(rows) counts VEHICLES. The headcount must count PEOPLE. Two runs of
// different sizes make the two numbers differ, which is the only fixture
// shape in which confusing them is visible at all.
func TestTheOnRoadHeadcountCountsPeopleNotVehicles(t *testing.T) {
	w := newRoadWorld(t) // TRK-100: 3 people

	second := []domain.Person{}
	for _, n := range []string{"Tanaka, Rin", "Ualdo, Bea"} {
		p, err := domain.CreatePerson(w.st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		second = append(second, p)
	}
	dispatchToPad(t, w.st, "SNOWCAT 2", second, 45)

	page := styleRe.ReplaceAllString(rawPage(t, w.srv, "/muster/print"), "")
	if !strings.Contains(page, `data-tally-count="in-transit">5<`) {
		t.Errorf("the headcount does not read 5 people on the road (it may be counting the 2 vehicles):\n%s", page)
	}
	// ⟦LOCK — §71(b), 2026-09-03⟧ ⟦was: "if !strings.Contains(page, `data-in-transit-vehicle=\"`+v+`\"`)"⟧
	// Same move as above: presence is asserted on the LABEL, which is what names the run to a
	// reader. The identity hook cannot carry a pinnable value and is asserted by discrimination.
	for _, v := range []string{"TRK-100", "SNOWCAT 2"} {
		if !strings.Contains(page, "<code>"+v+"</code>") {
			t.Errorf("run %s is missing from the sheet", v)
		}
	}
}

// --- what the sheet may and may not claim -----------------------------

// The honesty assertion, and the reason it is a test rather than a comment.
// The post that DISPATCHED a run does not hold its clock: OnApplied
// captures only trips whose destination_site is this site, so an origin has
// no deadline_capture row for its own outbound trip, and no wall clock
// rides the replication envelope (§9.3). This sheet must therefore say
// where the clock is rather than imply it has one — an unanchored deadline
// rendered as a countdown is exactly what §9.3 refuses.
func TestTheRoadSectionSaysWhereTheClockActuallyIs(t *testing.T) {
	w := newRoadWorld(t)
	sec := roadSection(t, rawPage(t, w.srv, "/muster/print"))
	// ⟦AMENDED — James, 2026-10-04 (ROAD-NOTE, FINDINGS-R4 L10)⟧ ⟦was: "kept at the
	// destination post"⟧ — that sentence said the arrival clock lives ONLY at the
	// destination, contradicting the origin's own OUTBOUND alert (feda689). The pin
	// now holds the TRUE pair: the origin's alert exists, and the destination keeps
	// its own clock. The lateness disclaimer below is unchanged.
	if !strings.Contains(sec, "OUTBOUND alert on this post") || !strings.Contains(sec, "destination keeps its own clock") {
		t.Errorf("the section does not say where the arrival clocks live:\n%s", sec)
	}
	if !strings.Contains(sec, "cannot say whether a run is late") {
		t.Errorf("the section does not disclaim the lateness verdict it cannot make:\n%s", sec)
	}
	// It must not present the allowance as a due time. "recorded at
	// dispatch" is the phrase that makes 90 minutes an allowance rather
	// than a countdown.
	if !strings.Contains(sec, "allowance") {
		t.Errorf("the deadline figure is not labelled as an allowance — it reads as a live countdown:\n%s", sec)
	}
}

// It is NOT a warning, and the contrast that matters is with
// .incomplete-warn, which IS one. Both boxes reach paper and both are
// solid, so on a one-colour sheet the WORDS have to do the work: the
// warning wears .cat-tag-warn and says the sheet may be short; this one
// wears a plain .cat-tag and says the sheet knows exactly where these
// people are. A reader must not treat a crew on the road as a defect.
func TestTheRoadSectionDoesNotReadAsAWarning(t *testing.T) {
	w := newRoadWorld(t)
	sec := roadSection(t, rawPage(t, w.srv, "/muster/print"))
	if strings.Contains(sec, "cat-tag-warn") {
		t.Errorf("the ON THE ROAD section wears the WARNING tag — these people are fine, they are on the road:\n%s", sec)
	}
	if strings.Contains(sec, "cat-tag-unwatched") {
		t.Errorf("the ON THE ROAD section wears the NOTHING-IS-WATCHING tag — the destination post's deadline board IS watching:\n%s", sec)
	}
	if !strings.Contains(sec, "nothing here is a warning") {
		t.Errorf("the section does not say in words that it is not a warning:\n%s", sec)
	}
	// Precondition, so this is a real contrast and not a claim about an
	// element that does not exist: the warning tag is a thing this page
	// renders when it has something to warn about.
	warnSrv, warnStore := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(warnStore, "site-a", 1, "Lee Late", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, warnStore, "site-a", "TRUCK 9", p.PersonID)
	if !strings.Contains(styleRe.ReplaceAllString(rawPage(t, warnSrv, "/muster/print"), ""), "cat-tag-warn") {
		t.Fatal("precondition: the sheet never renders cat-tag-warn at all, so its absence above proves nothing")
	}
}

// --- print --------------------------------------------------------------

// It PRINTS. Resolved through the cascade, not by grepping one line: these
// are people this operation put on the road, so the paper itself has to
// carry them. This is the exact test the away ruling answers in the other
// direction, and the two are asserted separately on purpose.
func TestPrintNeverHidesThePeopleThisPostPutOnTheRoad(t *testing.T) {
	w := newRoadWorld(t)
	rules := paperStylesheet(t, rawPage(t, w.srv, "/muster/print"))
	// Precondition: the print path hides SOMETHING, or "it does not hide
	// this" is true of the wrong stylesheet.
	if _, hidden := hiddenOnPaper(rules, "topbar"); !hidden {
		t.Fatal("precondition: print hides nothing at all — this test is not reading the print rules")
	}
	if why, hidden := hiddenOnPaper(rules, "on-road"); hidden {
		t.Fatalf("print hides .on-road — under D-6 these are people THIS operation is responsible for and the paper must carry them. Resolved as %s", why)
	}
}

// On paper the hue is gone, so the BOX FORM has to keep the categories
// apart: .on-road is SOLID (something is watching these runs — the
// destination's board) and .not-tracked is DASHED (nothing is). If they
// ever converge, the one distinction that survives a one-colour sheet is
// gone.
func TestOnPaperTheRoadBoxIsNotTheNotTrackedBox(t *testing.T) {
	w := newRoadWorld(t)
	rules := paperStylesheet(t, rawPage(t, w.srv, "/muster/print"))
	road, ok := paperClassValue(rules, "on-road", "border")
	if !ok {
		t.Fatal("no border resolves for .on-road on paper — it would print as an unmarked panel")
	}
	notTracked, ok := paperClassValue(rules, "not-tracked", "border")
	if !ok {
		t.Fatal("no border resolves for .not-tracked on paper")
	}
	if !strings.Contains(road, "solid") {
		t.Errorf(".on-road does not print a solid box; border resolves to %q", road)
	}
	if strings.Contains(road, "dashed") {
		t.Errorf(".on-road prints dashed, the form reserved for a gap in knowledge; border resolves to %q", road)
	}
	if road == notTracked {
		t.Errorf(".on-road and .not-tracked print the SAME box (%q) — a known crew and an unknown whereabouts would be indistinguishable on paper", road)
	}
}

// Reading order IS the design, and this section's PLACE in it is the design
// decision: between ON SITE and NOT ON SITE, because it is the second half
// of "people this operation is responsible for" and not a third kind of
// off-site exception. Filing our own crew inside the box that also holds
// Conoco's people would erase the distinction James's away ruling drew.
func TestThePrintedSheetPutsTheRoadBetweenOnSiteAndNotOnSite(t *testing.T) {
	w := newRoadWorld(t)
	body := rawPage(t, w.srv, "/muster/print")
	order := []string{"tally", "on-site", "in-transit", "off-site"}
	prev, prevName := -1, ""
	for _, sec := range order {
		at := strings.Index(body, `data-muster-section="`+sec+`"`)
		if at < 0 {
			t.Fatalf("printed sheet has no %q section:\n%s", sec, body)
		}
		if at < prev {
			t.Fatalf("section %q reads before %q — the sheet must run: %v", sec, prevName, order)
		}
		prev, prevName = at, sec
	}
	// ...and it is a SIBLING of the off-site box, never nested inside it.
	road := strings.Index(body, `data-muster-section="in-transit"`)
	off := strings.Index(body, `data-muster-section="off-site"`)
	if road > off {
		t.Fatal("the road section is inside or after the off-site section")
	}
	if end := strings.Index(body[road:], "</section>"); end < 0 || road+end > off {
		t.Fatal("the ON THE ROAD section is not closed before the NOT ON SITE section opens — one is nested in the other")
	}
}

// The hooks this file stands on must stay style-free, for the same reason
// every other muster hook does (F19): if the stylesheet ever selects on one,
// an assertion about a rendered element silently becomes a stylesheet match.
func TestTheOnRoadStructuralHooksAreNeverStyled(t *testing.T) {
	w := newRoadWorld(t)
	css := stylesheetOf(t, rawPage(t, w.srv, "/muster"))
	for _, hook := range []string{"data-in-transit-vehicle", `data-tally-row="in-transit"`, `data-muster-section="in-transit"`} {
		if strings.Contains(css, hook) {
			t.Errorf("the stylesheet selects on %s — it is a test hook and must stay style-free", hook)
		}
	}
}

// An empty category still has to say so. A sheet that silently omits the
// section when nobody is on the road leaves a reader unable to tell
// "checked, nobody" from "not checked" — the F1 lesson, applied to the
// fourth population. The COUNT is the thing that must always print; the
// named block appears only when there is somebody to name.
func TestASheetWithNobodyOnTheRoadStillStatesTheCategory(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	for _, route := range []string{"/muster", "/muster/print"} {
		page := styleRe.ReplaceAllString(rawPage(t, srv, route), "")
		if !strings.Contains(page, `data-tally-count="in-transit">0<`) {
			t.Errorf("%s: the headcount omits the on-the-road category entirely when it is empty:\n%s", route, page)
		}
		if hasRoadSection(page) {
			t.Errorf("%s: an empty road section is rendered as a named block", route)
		}
	}
}

// The allowance renders singular and plural correctly, asserted directly
// because the fixture above only ever has a 90-minute run and "1 minutes"
// cannot appear in it either way. This is the leg that would have caught the
// defect measured on paper on 2026-08-21.
func TestTheJourneyAllowanceReadsGrammaticallyAtEveryValue(t *testing.T) {
	for _, tc := range []struct {
		mins *int64
		want string
	}{
		{i64(90), "90 minutes"},
		{i64(1), "1 minute"},
		{i64(0), "0 minutes"},
		{nil, ""},
	} {
		got := domain.InTransitRow{DeadlineMinutes: tc.mins}.Allowance()
		if got != tc.want {
			t.Errorf("allowance for %v = %q, want %q", tc.mins, got, tc.want)
		}
	}
}

func i64(n int64) *int64 { return &n }
