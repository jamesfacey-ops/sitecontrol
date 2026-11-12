// w6_seams_test.go — W6 (item 5, the visit name snapshot): shared fixture
// machinery for the domain-side W6 criteria. NO ASSERTIONS LIVE HERE.
//
// ⚠ THE ONE DESIGN DECISION IN THIS FILE, stated first because every other
// W6 domain test rests on it.
//
// W6 is greenfield in its own artefacts: at f33c4c7 there is no
// `person_snapshot_name` column and no `PersonSnapshotName` Go field
// (plan:5023 §2.1 — zero occurrences of either, anywhere under app/).
// A test that NAMES the Go field does not fail — it fails to COMPILE, and a
// compile error in `package domain` buries every other W6 red behind one
// message. W5a met the same fork and recorded the answer
// (`reports/window-build/W5a-tests.md` §1.1).
//
// So NOTHING in W6's domain tests names an unbuilt Go symbol. The snapshot
// is read and written through its FROZEN JSON TAG — `person_snapshot_name`
// on `Visit`, `person_name` on `ArrivalVisit` (DR-W6-4's four literals,
// plan:5023 §DR-W6-4) — by marshalling the real struct the real reader
// returns. Consequences, all of them wanted:
//
//   - every W6 domain test COMPILES AND RUNS against f33c4c7, so its
//     pre-chunk failure is a produced RUNTIME red naming its criterion,
//     never a compile error;
//   - the read still goes through `domain.VisitByID` / `OpenVisitsAt`, as
//     W6-1(b) requires, and never through an HTTP dump ([[D-56]]);
//   - the JSON tag is asserted by construction rather than by reading a
//     struct tag, which is what W6-6(a) and W6-7(b) actually claim;
//   - the builder's Go field name is left to the builder, and W6-6(a)'s
//     Go-field assertion is made by REFLECTION (w6_projection_test.go),
//     which is also a runtime check.
//
// ⚠ REPORTED, NOT HIDDEN: the plan's §6 RED row predicts that W6-5's frozen
// rows (ii) and (iii) can only red as COMPILE errors, "because their fixture
// names the unbuilt PersonSnapshotName". This machinery makes them red at
// RUNTIME instead. That is strictly more evidence, not a criterion change —
// the criterion is the value in the table, not the compiler's opinion — but
// a W6 gate expecting a compile error must know it will meet a runtime
// failure instead. See reports/window-build/W6-tests.md §1.2.
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// --- the frozen wire spellings, as literals (DR-W6-4) --------------------

const (
	// w6VisitSnapshotKey is the `visit` projection's JSON tag == its column
	// name (plan:5023 DR-W6-4: "The projection struct's tags ARE its column
	// names").
	w6VisitSnapshotKey = "person_snapshot_name"
	// w6ArrivalNameKey is the PAYLOAD spelling on ArrivalVisit — deliberately
	// NOT the column spelling, sibling of company_name/workgroup_name.
	w6ArrivalNameKey = "person_name"
	// w6GhostColumnKey / w6GhostPayloadKey are DR-W6-4's GHOST RULE: the
	// rejected spelling must not appear on the other side.
	w6GhostColumnKey  = "person_name"          // must NOT be a `visit` column
	w6GhostPayloadKey = "person_snapshot_name" // must NOT be a payload key
)

func w6Str(s string) *string { return &s }

// --- reading the snapshot off a real Visit -------------------------------

// w6SnapshotOf reports the visit row's person_snapshot_name as (value,
// present). `present` is false when the key is absent or JSON null — the
// two spellings of "no snapshot" — and true for the empty string, which
// DR-W6-3 rules ABSENT at the RENDER, not at the ROW. The distinction is
// deliberate: this helper reports what the row carries, and the render rule
// is asserted where the render happens.
func w6SnapshotOf(t *testing.T, v Visit) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("w6: marshalling a Visit failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("w6: a marshalled Visit did not decode as an object: %v", err)
	}
	val, ok := m[w6VisitSnapshotKey]
	if !ok || val == nil {
		return "", false
	}
	s, isStr := val.(string)
	if !isStr {
		t.Fatalf("w6: Visit.%s decoded as %T (%v), want a JSON string or null — "+
			"DR-W6-4 freezes the Go type as *string", w6VisitSnapshotKey, val, val)
	}
	return s, true
}

// w6VisitKeys returns the marshalled key set of a Visit — W6-7(a)'s shape
// at the projection-struct level.
func w6VisitKeys(t *testing.T, v Visit) []string {
	t.Helper()
	return w6JSONKeys(t, v)
}

func w6JSONKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("w6: marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("w6: unmarshal: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- constructing Visit values that CARRY a snapshot ---------------------
//
// W6-5's two tables need visits whose snapshot is "", "   ", a real name, or
// absent, against a live roster that says something different. The struct
// field does not exist at f33c4c7, so the value is injected through the
// frozen JSON tag: at f33c4c7 the key is unknown and json.Unmarshal drops it,
// the visit carries no snapshot, the frozen closure falls through to the live
// name — and the pinned literal reds. That IS the produced red for those
// rows; it is not an instrument failure.

// w6VisitWithSnapshot builds an OPEN visit for personID in one
// company/workgroup bucket, carrying the given snapshot. snapshot == nil
// means the column is NULL (absent).
func w6VisitWithSnapshot(t *testing.T, visitID, personID string, snapshot *string) Visit {
	t.Helper()
	// ⟦LOCK — §42.4.4, 2026-09-03 (S102B)⟧ THE TWO TRADE KEYS, RENAMED AT LAST, and the
	// silent-drop closed underneath them so this cannot recur unseen.
	// ⟦was: "\"workgroup_snapshot_id\":   \"wg-1\","⟧
	// ⟦was: "\"workgroup_snapshot_name\": \"Rig Crew\","⟧
	//
	// ⚠ WHY THE PREVIOUS PASS ROUTED THIS RATHER THAN DOING IT, and which of its three reasons
	// survived measurement (its own words are kept below the line):
	//   (a) "it clears NO compile error, so it is outside this pass's remit" — TRUE THEN, and
	//       it is the remit of THIS one.
	//   (b) "these literals ARE an assertion, not plumbing" — ⭐ TRUE OF THE SNAPSHOT KEY,
	//       NOT OF THESE TWO. The frozen-tag assertion this file's header describes is
	//       DR-W6-4's FOUR literals, and every one of them is a named constant above
	//       (w6VisitSnapshotKey / w6ArrivalNameKey / the two ghosts). The company and trade
	//       keys are inline scenery that places a visit in a bucket; nothing asserts their
	//       spelling. So (b) was over-broad, and the correction is recorded rather than
	//       assumed.
	//   (c) "SHARED HELPER MACHINERY … SEVEN further call sites" — TRUE, and it is why the
	//       per-call-site effect was MEASURED rather than reasoned about. See
	//       reports/window-build/S102B-W6-FIXTURE.md: exactly one assertion outcome changes.
	//
	// ⭐ MEASURED before the rename (paired probe, one visit each way, all else identical):
	//     stale keys -> co("co-1","Northwind Drilling"){ tr("","(no trade recorded)")x2 }
	//     fresh keys -> co("co-1","Northwind Drilling"){ tr("wg-1","Rig Crew")x2         }
	//     w6BoardNames: IDENTICAL for both, in the live AND the frozen closure.
	// ⚠ So w6BoardNames could not see this, and the only reason the tree was not silently
	// green is that w6_closures_test.go:329 asserts the GROUPING directly rather than through
	// the name list. That assertion was RED at a2c2db6 and named the symptom.
	m := map[string]any{
		"visit_id":              visitID,
		"site_id":               "site-a",
		"person_id":             personID,
		"trip_id":               nil,
		"company_snapshot_id":   "co-1",
		"company_snapshot_name": "Northwind Drilling",
		"trade_snapshot_id":     "wg-1",
		"trade_snapshot_name":   "Rig Crew",
		"is_open":               1,
		"recorded_by":           nil,
	}
	if snapshot == nil {
		m[w6VisitSnapshotKey] = nil
	} else {
		m[w6VisitSnapshotKey] = *snapshot
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var v Visit
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("w6: could not build a Visit from the frozen wire shape: %v", err)
	}
	w6RequireNoKeyWasDropped(t, m, v)
	return v
}

// w6RequireNoKeyWasDropped is ⭐ THE DURABLE HALF OF §42.4.4, and it is the
// instrument that would have caught the defect the day it was created.
//
// ⚠ THE DEFECT CLASS, stated so it is checkable rather than remembered:
// this fixture builds its Visit by MARSHALLING A MAP and unmarshalling it
// into the real struct — which is the whole point, because it lets a test
// assert the frozen JSON tag BY CONSTRUCTION without naming an unbuilt Go
// field. ⛔ But `encoding/json` DISCARDS an object key that matches no
// field, IN SILENCE. So a production rename (`workgroup_snapshot_*` →
// `trade_snapshot_*`, W8) turns a key this fixture writes into a key nothing
// reads, and every visit it builds quietly loses that value. [MEASURED at
// a2c2db6: every visit fell into the "(no trade recorded)" bucket, and
// w6BoardNames — which seven assertions are phrased over — returns the
// IDENTICAL list either way.]
//
// ⚠ THE BAR IS BY SHAPE, NOT BY SPELLING. It does not enumerate
// "trade_snapshot_id, trade_snapshot_name": an enumeration goes short at
// exactly the next rename, which is the failure it exists to prevent. It
// asserts the PROPERTY — every key this fixture writes must still be a key
// the struct round-trips — so a field renamed in production reds HERE, at
// the fixture, naming the key, instead of surfacing as a puzzling grouping
// change in a distant assertion or as nothing at all.
func w6RequireNoKeyWasDropped(t *testing.T, wrote map[string]any, v Visit) {
	t.Helper()
	// ⚠ SORTED, both sides. w6VisitKeys ranges a map, so an unsorted list
	// reorders between runs and two readings of the same failure would not
	// look like the same failure.
	round := w6VisitKeys(t, v)
	sort.Strings(round)
	kept := map[string]bool{}
	for _, k := range round {
		kept[k] = true
	}
	var dropped []string
	for k := range wrote {
		if !kept[k] {
			dropped = append(dropped, k)
		}
	}
	if len(dropped) == 0 {
		return
	}
	sort.Strings(dropped)
	t.Fatalf("⛔ FIXTURE KEY SILENTLY DROPPED ⟦§42.4.4⟧: this fixture wrote %v, and `Visit` round-trips "+
		"no such key — encoding/json discarded the value without a word.\n"+
		"  ⚠ This is a FIXTURE DEFECT, not a criterion failure: every visit built here is missing that "+
		"value, so the case this fixture exists to pose is not being posed.\n"+
		"  ⚠ Do NOT re-pin the assertion that noticed. The usual cause is a PRODUCTION RENAME "+
		"(trip.go's `Visit` tags) that this fixture's literals did not follow.\n"+
		// w6SortedKeys is w6_populate_test.go:72's — reused rather than re-declared.
		"  keys written: %v\n  keys Visit round-trips: %v", dropped, w6SortedKeys(wrote), round)
}

// --- boards: the ordered names a sheet would print -----------------------

// w6BoardNames is the ordered list of PersonName values a board would print:
// the roster body first (company order, then workgroup order, then member
// order), then the pooled Summary. This is the DOMAIN-level twin of W6-X2's
// carrying elements; the rendered-element assertions live in internal/web.
func w6BoardNames(b MusterBoard) []string {
	var out []string
	for _, c := range b.Companies {
		// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ W8 Workgroup→Trade rename: the board's
		// nesting SURVIVED the rename — CompanyGroup still carries Trades []TradeGroup
		// (muster.go:75), so company→trade→member order is structurally unchanged.
		// ⟦was: "for _, w := range c.Workgroups {"⟧
		for _, w := range c.Trades {
			for _, m := range w.Members {
				out = append(out, m.PersonName)
			}
		}
	}
	for _, m := range b.Summary {
		out = append(out, m.PersonName)
	}
	return out
}

func w6Equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// w6Persons builds the LIVE roster index input for the two closures.
func w6Persons(pairs ...[2]string) []Person {
	out := make([]Person, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, Person{PersonID: p[0], Name: p[1]})
	}
	return out
}

var (
	w6Companies = []Company{{CompanyID: "co-1", Name: "Northwind Drilling"}}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ W8 Workgroup→Trade rename.
	// ⚠ CompanyID IS DROPPED, NOT TRANSCRIBED — Trade has no such field (refdata.go:202-214);
	// a trade belongs to a SITE, a workgroup belonged to a COMPANY. ⭐ MEASURED, NOT INFERRED
	// (probe over an isolated copy, all other domain tests held aside): the parent pointer was
	// NEVER what nested trades under companies. MusterLive uses this slice ONLY as an id→name
	// lookup (`tradeName[w.TradeID] = w.Name`, muster.go:203-206); the company→trade nesting
	// comes from each Visit's own CompanySnapshotID + TradeSnapshotID pair. So nothing that was
	// ever read is lost. ⭐ SiteID is likewise NEVER READ: the produced board is byte-identical
	// for "site-a", "site-ZZZ" and "" — "site-a" is chosen only for consistency with the visits.
	// ⟦was: "w6Workgroups = []Workgroup{{WorkgroupID: \"wg-1\", CompanyID: \"co-1\", Name: \"Rig Crew\"}}"⟧
	w6Trades = []Trade{{TradeID: "wg-1", SiteID: "site-a", Name: "Rig Crew"}}
)

// w6LiveBoard drives the LIVE closure with the 7-argument signature W5d
// landed (plan:5023 §2.6: MusterLive has SEVEN parameters; the fifth
// positional slot became two).
func w6LiveBoard(visits []Visit, persons []Person) MusterBoard {
	return MusterLive(visits, persons, w6Companies, w6Trades, AliasMap{}, AliasMap{}, 0)
}

func w6FrozenBoard(visits []Visit, persons []Person) MusterBoard {
	return MusterReprint(visits, persons, 0)
}

// --- store-side fixture helpers ------------------------------------------

func w6NewStore(t *testing.T) *store.Store { return w3NewStore(t) }

// w6OpenVisits reads siteID's open visits back through the production
// reader ([[D-56]]: never through an HTTP dump).
func w6OpenVisits(t *testing.T, st *store.Store, siteID string) []Visit {
	t.Helper()
	vs, err := OpenVisitsAt(st, siteID)
	if err != nil {
		t.Fatal(err)
	}
	return vs
}

// w6VisitFor finds the open visit belonging to personID, failing loudly
// rather than returning a zero value.
func w6VisitFor(t *testing.T, st *store.Store, siteID, personID string) Visit {
	t.Helper()
	for _, v := range w6OpenVisits(t, st, siteID) {
		if v.PersonID == personID {
			// Re-read by primary key too: W6-1(b) names BOTH readers.
			byID, err := VisitByID(st, v.VisitID)
			if err != nil {
				t.Fatal(err)
			}
			if byID == nil {
				t.Fatalf("w6: VisitByID(%s) returned nil for a visit OpenVisitsAt just listed", v.VisitID)
			}
			got, _ := w6SnapshotOf(t, *byID)
			want, _ := w6SnapshotOf(t, v)
			if got != want {
				t.Fatalf("w6: the two readers disagree about visit %s's %s: VisitByID=%q OpenVisitsAt=%q",
					v.VisitID, w6VisitSnapshotKey, got, want)
			}
			return v
		}
	}
	t.Fatalf("w6: no open visit for person %s at %s — the check-in fixture did not land", personID, siteID)
	return Visit{}
}

// w6CheckInExpected drives the EXPECTED arrival entry point end to end
// (DispatchLocalCheckIn + RecordExpectedArrival — the degenerate-trip
// pattern muster_test.go already uses).
func w6CheckInExpected(t *testing.T, st *store.Store, siteID string, epoch int64, driver string, occupants []string) {
	t.Helper()
	trip, err := DispatchLocalCheckIn(st, siteID, epoch, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-14", DriverPersonID: driver, OccupantPersonIDs: occupants,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "TRK-14"), DriverPersonID: driver, OccupantPersonIDs: occupants,
	}, 0, "default")
	if err != nil {
		t.Fatalf("w6 fixture: dispatch: %v", err)
	}
	if _, err := RecordExpectedArrival(st, siteID, epoch, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w6 fixture: expected arrival: %v", err)
	}
}

// w6CheckInUnsolicited drives the SECOND arrival entry point end to end.
func w6CheckInUnsolicited(t *testing.T, st *store.Store, siteID string, epoch int64, personIDs []string) {
	t.Helper()
	// ⚠ ⟦ROUTED — §50 arrival re-seed, §47 condition 3 (P4), 2026-09-03⟧ NOT RE-SEEDED, AND THE
	// REASON WAS FOUND BY §49's LIMB 4(b) — the caller enumeration, not the diff. THIS HELPER IS
	// SHARED: `w6_scoped_read_test.go`, `w6_populate_test.go` (4 sites) and `w6_frozenarm_test.go`
	// call it and NONE has a `RecordUnsolicitedArrival` site of its own, so their assertion diff
	// is EMPTY. ⚠ `w6_scoped_read_test.go` wraps THIS CALL in `w6Measure` and pins the rows
	// decoded as LITERALS — "the number is pinned as a LITERAL and not as a delta, deliberately".
	// `w7SeedVehicleID` reads `Vehicles(st)` and AUTHORS a `vehicle_created` event INSIDE that
	// window, which is exactly the event-stream-shape dependency P4 excludes.
	// ⚠ AND THE TRANSFORMATION WOULD RESTORE NOTHING HERE [MEASURED]: no caller asserts any
	// disposition, and the only trip in scope is `w6CheckInExpected`'s DEGENERATE "TRK-14", which
	// `trip.go` excludes from candidacy outright — so this arrival correlates with nothing before
	// or after. All cost, no repair. See reports/window-build/ARRIVAL-RESEED.md.
	if _, err := RecordUnsolicitedArrival(st, siteID, epoch, "BUS 9", "site-b", personIDs, nil, 0, "default"); err != nil {
		t.Fatalf("w6 fixture: unsolicited arrival: %v", err)
	}
}

// w6AmendName amends one person's display name through W3's surface — the
// live half of W6-2's regression.
func w6AmendName(t *testing.T, st *store.Store, siteID string, epoch int64, personID, name string) {
	t.Helper()
	if _, err := AmendPerson(st, siteID, epoch, PersonAmendment{
		PersonID: personID, Name: w6Str(name),
	}, 0, "default"); err != nil {
		t.Fatalf("w6 fixture: amending %s to %q was refused: %v", personID, name, err)
	}
}

// w6LiveName reads the COMPOSED person's current display name.
func w6LiveName(t *testing.T, st *store.Store, personID string) string {
	t.Helper()
	p, err := PersonByID(st, personID)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("w6: person %s has no roster row", personID)
	}
	return p.Name
}

// w6AllPersons is the live roster the muster's callers pass.
func w6AllPersons(t *testing.T, st *store.Store) []Person {
	t.Helper()
	ps, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func w6Fmt(names []string) string { return fmt.Sprintf("%q", names) }
