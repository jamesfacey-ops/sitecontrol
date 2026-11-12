// w6_populate_test.go — W6-1(b) (every visit is born carrying its person's
// name), W6-1(b)'s two named rivals, and W6-7(b) / W6-6(a)'s wire half.
//
// W6-1(b): "Both arrival entry points, driven end-to-end, produce a `visit`
// row whose `person_snapshot_name` equals the roster's COMPOSED `Name` AT
// AUTHORING TIME, read back through domain.VisitByID / OpenVisitsAt."
//
// Two rivals answer identically on the NATURAL fixture and are named
// because of it:
//
//	(m6-1c) the populate reading the name PARTS — composeName(LastName,
//	        FirstName) — instead of Name. For any intake-created person the
//	        two are byte-identical (intake.go:231-241) and an amendment does
//	        not break the identity, because it leaves the parts stale. The
//	        consequence is PERMANENT: no `UPDATE visit` anywhere in the tree
//	        ever rewrites a snapshot (checkin.go:104, trip.go:337, :420 set
//	        `is_open` and nothing else). So the fixture person's parts
//	        compose to a DIFFERENT string than Name — the divergence [[D-48]]
//	        exists to permit.
//	(m6-1d) an EPOCH-BLIND resolution at the freeze point. The §5 standing
//	        cross-epoch CONTROL row binds here and is met BY FIXTURE, in
//	        W4-12's form: two competing `name` amendments for one person at
//	        ONE site — (site-a, epoch 2, seq 1) and (site-a, epoch 1, seq 99),
//	        the re-image shape — the check-in authored AFTER both, and the
//	        resulting snapshot PINNED as the epoch-2 literal. Not
//	        permutation-invariance: an epoch-blind comparator can be
//	        perfectly invariant and still wrong.
package domain

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w6ArrivalVisitPayloads returns the marshalled `visits` objects of every
// trip_arrival authored on the (siteID, epoch) stream — the REAL wire bytes,
// not a struct tag.
func w6ArrivalVisitPayloads(t *testing.T, st *store.Store, siteID string, epoch int64) []map[string]any {
	t.Helper()
	evs, err := st.Events(siteID, epoch, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		var probe struct {
			Type   string            `json:"type"`
			Visits []json.RawMessage `json:"visits"`
		}
		if err := json.Unmarshal(e.Payload, &probe); err != nil {
			continue
		}
		if probe.Type != "trip_arrival" {
			continue
		}
		for _, raw := range probe.Visits {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("w6: an ArrivalVisit did not decode as an object: %v", err)
			}
			out = append(out, m)
		}
	}
	return out
}

func w6SortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- W6-1(b): both doors, both rivals -----------------------------------
//
// EXPECTED RED at f33c4c7: every sub-test. The `visit` row carries no
// `person_snapshot_name` at all, so `present=false` and the pinned literal
// cannot be met. The failure shape names the criterion, the door, the
// expected literal and the rival it separates.
func TestW6EveryVisitIsBornCarryingItsPersonsName(t *testing.T) {
	const (
		certifiedName = "Vega, Ana"
		// The PARTS deliberately compose to a different string (D-48's
		// permitted divergence). composeName(last, first) == "Nakamura, Kenji".
		partsLast, partsFirst = "Nakamura", "Kenji"
		rivalComposed         = "Nakamura, Kenji"
	)

	for _, door := range []struct {
		name string
		in   func(t *testing.T, st *store.Store, personID string)
	}{
		{"expected-arrival", func(t *testing.T, st *store.Store, personID string) {
			w6CheckInExpected(t, st, "site-a", 1, personID, nil)
		}},
		{"unsolicited-arrival", func(t *testing.T, st *store.Store, personID string) {
			w6CheckInUnsolicited(t, st, "site-a", 1, []string{personID})
		}},
	} {
		door := door
		t.Run(door.name, func(t *testing.T) {
			st := w6NewStore(t)
			co, err := CreateCompany(st, "site-a", 1, "Northwind Drilling", 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := CreateTrade(st, "site-a", 1, co.CompanyID, "Rig Crew", 0, "default")"⟧
			wg, err := CreateTrade(st, "site-a", 1, "Rig Crew", 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			p, err := CreatePersonWithDetails(st, "site-a", 1, certifiedName,
				&co.CompanyID, &wg.TradeID, nil,
				PersonDetails{LastName: w6Str(partsLast), FirstName: w6Str(partsFirst)},
				0, "default")
			if err != nil {
				t.Fatal(err)
			}

			// --- the fixture asserts its own arrangement (rule 6) --------
			// Without the divergence below this test agrees with (m6-1c) and
			// certifies nothing: a populate reading the parts would pass.
			live := w3Person(t, st, p.PersonID)
			if live.Name != certifiedName {
				t.Fatalf("PRECONDITION: the composed person's Name is %q, want %q", live.Name, certifiedName)
			}
			if live.LastName == nil || live.FirstName == nil {
				t.Fatalf("PRECONDITION: the fixture person must CARRY both name parts, or (m6-1c) is not posed; "+
					"got last=%v first=%v", live.LastName, live.FirstName)
			}
			if got := *live.LastName + ", " + *live.FirstName; got != rivalComposed || got == certifiedName {
				t.Fatalf("PRECONDITION: the parts must compose to a DIFFERENT string than Name — "+
					"that divergence is what separates the certified populate from (m6-1c). "+
					"parts compose to %q, Name is %q", got, certifiedName)
			}

			door.in(t, st, p.PersonID)

			v := w6VisitFor(t, st, "site-a", p.PersonID)
			got, present := w6SnapshotOf(t, v)
			if !present || got != certifiedName {
				t.Errorf("W6-1(b) (m6-1b, m6-1c): the %s door produced a visit whose %s is %q (present=%v), want %q.\n"+
					"  Every visit must be born carrying its person's COMPOSED Name, read back through "+
					"OpenVisitsAt/VisitByID (never an HTTP dump, [[D-56]]).\n"+
					"  present=false is (m6-1b) — the arm kept and the POPULATE dropped, or the column not built at all.\n"+
					"  a value of %q is (m6-1c) — the populate read the name PARTS instead of Name. That is byte-identical "+
					"to the certified answer for every intake-created person, and no UPDATE visit anywhere in the tree "+
					"ever rewrites a snapshot, so it would be permanent.",
					door.name, w6VisitSnapshotKey, got, present, certifiedName, rivalComposed)
			}
		})
	}
}

// --- W6-1(b): the §5 standing CROSS-EPOCH control row, met by fixture ---
//
// It binds harder here than anywhere it has bound before, and that is
// measured, not asserted: person_amend.go:279 is the one ordering spelling
// (`ORDER BY site_id, node_epoch, seq`), PersonByID composes over it,
// tripauthor.go:404 IS PersonByID, and `p.Name` is exactly what W6 freezes —
// while the only three `UPDATE visit` statements in the tree set `is_open`
// and nothing else. So W6 converts an ordering error from ONE WRONG RENDER
// into EVERY FUTURE PRINTED SHEET FOR THAT VISIT, permanently.
//
// EXPECTED RED at f33c4c7: yes — `present=false`, the column does not exist.
// After the build, a red naming the epoch-1/seq-99 name is (m6-1d).
func TestW6SnapshotFreezesTheEpochMajorWinnerNotTheHighestSeq(t *testing.T) {
	const (
		createdName = "Vega, Ana"
		loserName   = "LOSER epoch1 seq99"
		winnerName  = "Vega, Reimaged" // (site-a, epoch 2, seq 1) — the PINNED literal
	)
	st := w6NewStore(t)
	p, err := CreatePerson(st, "site-a", 1, createdName, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// Inflate epoch 1 to watermark 98 so the LOSER lands at seq 99 EXACTLY
	// (W4-12's own technique — the store refuses a seq gap, so the literal
	// key has to be reached rather than declared).
	for w3Watermark(t, st, "site-a", 1) < 98 {
		if _, err := CreatePerson(st, "site-a", 1, "Epoch1 Filler", nil, nil, 0, "default"); err != nil {
			t.Fatal(err)
		}
	}
	// The LOSER: high seq, EARLIER epoch, LATER clock.
	st.Clock = w3Reading(w3Base + 3*w3Hour)
	loser, err := AmendPerson(st, "site-a", 1, PersonAmendment{
		PersonID: p.PersonID, Name: w6Str(loserName),
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// The WINNER: the re-image — epoch 2, seq 1, and an EARLIER clock than
	// the loser's, so a chronological comparator picks the wrong one.
	st.Clock = w3Reading(w3Base + 2*w3Hour)
	winner, err := AmendPerson(st, "site-a", 2, PersonAmendment{
		PersonID: p.PersonID, Name: w6Str(winnerName),
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// --- the fixture asserts its own arrangement (rule 6) ---------------
	if loser.Key() != (store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 99}) {
		t.Fatalf("FIXTURE DEFECT: the loser landed at %v, want the LOCKED literal (site-a, epoch 1, seq 99)", loser.Key())
	}
	if winner.Key() != (store.Key{SiteID: "site-a", NodeEpoch: 2, Seq: 1}) {
		t.Fatalf("FIXTURE DEFECT: the winner landed at %v, want the LOCKED literal (site-a, epoch 2, seq 1)", winner.Key())
	}
	if winner.OccurredAt >= loser.OccurredAt {
		t.Fatalf("FIXTURE DEFECT: the epoch-major WINNER must carry the EARLIER clock, or a chronological "+
			"comparator agrees with the certified answer and (m6-1d) is not posed; winner=%d loser=%d",
			winner.OccurredAt, loser.OccurredAt)
	}
	// ⚠ THE TWO WAYS THIS PRECONDITION CAN FAIL ARE DIFFERENT FINDINGS, and
	// they are separated here rather than both labelled "fixture defect".
	// [MEASURED by the W6 mutant gate, G-5] (m6-1d)'s LITERAL form is the
	// one-line change at person_amend.go:279 — PersonByID composes over that
	// single ordering spelling and tripauthor.go:404 IS PersonByID — so
	// applying the mutant moves the READ path too, and this precondition
	// fatals BEFORE the (m6-1d)-naming assertion below is ever reached.
	// Labelled "FIXTURE DEFECT … a W3 finding, not a W6 one", the produced
	// kill reads as something to dismiss. The kill is real; only the label
	// was wrong.
	if live := w6LiveName(t, st, p.PersonID); live != winnerName {
		if live == loserName {
			t.Fatalf("W6-1(b) / §5 cross-epoch CONTROL (m6-1d), variant A — THE SHARED ORDERING CONSTANT IS "+
				"EPOCH-BLIND: the composed roster name is %q, the epoch-1/seq-99 LOSER, want the epoch-2 "+
				"winner %q.\n"+
				"  ⚠ THIS IS NOT A FIXTURE DEFECT AND NOT A W3 FINDING. The fixture is arranged exactly as its "+
				"three assertions above certify; the COMPOSITION answered epoch-blind. It fatals here rather "+
				"than at the snapshot assertion below only because (m6-1d)'s literal form mutates the ONE "+
				"ordering spelling `ORDER BY site_id, node_epoch, seq` (person_amend.go:279), which the READ "+
				"path and the freeze point share — PersonByID composes over it and tripauthor.go:404 IS "+
				"PersonByID.\n"+
				"  Because no UPDATE visit anywhere in the tree ever rewrites a snapshot, this ordering error is "+
				"not one wrong render: it is every future printed sheet for this visit, permanently.",
				live, winnerName)
		}
		t.Fatalf("FIXTURE DEFECT: the composed roster name is %q, want %q — and it is not the epoch-1/seq-99 "+
			"loser %q either, so (m6-1d) does not explain it. The READ path's own ordering disagrees with this "+
			"fixture's premise in some third way, which is a W3 finding, not a W6 one",
			live, winnerName, loserName)
	}

	// The check-in is authored AFTER both amendments, on the epoch-2 stream.
	w6CheckInUnsolicited(t, st, "site-a", 2, []string{p.PersonID})

	v := w6VisitFor(t, st, "site-a", p.PersonID)
	got, present := w6SnapshotOf(t, v)
	if !present || got != winnerName {
		hint := ""
		switch got {
		case loserName:
			hint = " — that is (m6-1d): an EPOCH-BLIND resolution at the freeze point (chronological, or " +
				"seq-major across epochs) in the composition buildArrivalVisits reads"
		case createdName:
			hint = " — the snapshot froze the CREATED name: the populate did not compose the amendments at all"
		}
		t.Errorf("W6-1(b) / §5 cross-epoch CONTROL (m6-1d): the visit's %s is %q (present=%v), want the pinned "+
			"epoch-2 literal %q%s.\n"+
			"  The one ordering spelling is `ORDER BY site_id, node_epoch, seq` (person_amend.go:279); "+
			"PersonByID composes over it and tripauthor.go:404 IS PersonByID. Because no UPDATE visit ever "+
			"rewrites a snapshot, an ordering error here is not one wrong render — it is every future printed "+
			"sheet for this visit, permanently.",
			w6VisitSnapshotKey, got, present, winnerName, hint)
	}
}

// --- W6-7(b) / W6-6(a) wire half: the ArrivalVisit key set --------------
//
// [[D-60]]: "the visit name snapshot freezes the DISPLAY NAME and nothing
// else." Asserted STRUCTURALLY over the marshalled bytes of a REAL authored
// trip_arrival payload, so a later addition goes red rather than landing
// quietly — the shape w5d_payload_test.go:84 already uses.
//
// ⚠ THE GREENFIELD TRAP, and why this test is not trivially satisfied by an
// unbuilt chunk: "the payload carries no name parts" is true at f33c4c7
// because NOTHING EXISTS. The exact-set half is what anchors it: the
// permitted set has SEVEN members and the pre-chunk payload marshals at most
// SIX, so this REDS pre-chunk. A green here means "built, and correctly
// without parts" — never "not built".
//
// EXPECTED RED at f33c4c7: `person_name` missing from a fully-populated
// payload.
func TestW6ArrivalVisitPayloadKeySetIsExactlyTheSeven(t *testing.T) {
	permitted := []string{
		"visit_id", "person_id",
		"company_id", "company_name",
		"workgroup_id", "workgroup_name",
		w6ArrivalNameKey,
	}
	inPermitted := func(k string) bool {
		for _, p := range permitted {
			if p == k {
				return true
			}
		}
		return false
	}

	// Fixture A — every field populated: the EXACT set is the seven.
	//
	// ⚠ THE PERSON CARRIES EVERY ONE OF [[D-60]]'s FORBIDDEN PARTS, and that
	// is the whole reason this fixture is not a plain CreatePerson. [MEASURED
	// by the W6 mutant gate, G-2] with a person carrying NO parts, the
	// realistic (m6-7) plant on the payload shape SURVIVES: add a
	// `LastName *string` field tagged `last_name,omitempty` to ArrivalVisit,
	// populate it the way a builder would (`v.LastName = p.LastName`), and
	// all 22 W6 tests stay GREEN — because `p.LastName` is nil, `omitempty`
	// drops the key, and an exact-set assertion over MARSHALLED BYTES cannot
	// see a key that was never marshalled. So this test's own stated promise
	// — "a later addition goes red rather than landing quietly" — was FALSE
	// of the payload shape, which is exactly what [[D-60]]'s absence
	// criterion exists to guarantee. A fixture person carrying the parts is
	// what makes `omitempty` emit, and the plant visible.
	t.Run("all-seven-present", func(t *testing.T) {
		st := w6NewStore(t)
		co, err := CreateCompany(st, "site-a", 1, "Northwind Drilling", 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := CreateTrade(st, "site-a", 1, co.CompanyID, "Rig Crew", 0, "default")"⟧
		wg, err := CreateTrade(st, "site-a", 1, "Rig Crew", 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		p, err := CreatePersonWithDetails(st, "site-a", 1, "Vega, Ana",
			&co.CompanyID, &wg.TradeID, nil,
			PersonDetails{
				LastName:  w6Str("Nakamura"),
				FirstName: w6Str("Kenji"),
				Position:  w6Str("Rig Manager"),
				Phone:     w6Str("555 0100"),
			},
			0, "default")
		if err != nil {
			t.Fatal(err)
		}
		// ⟦SchemaVersion 11, 2026-09-05⟧ The `pad_oriented` arm that stood here
		// (asserted onto the person by amendment, as the one forbidden part
		// with no created-state door) is gone with the fact: D-88's ERRATA
		// struck the column from person, person_amendment and both payloads,
		// so it can no longer be a member of D-60's forbidden set. Nothing
		// else about this fixture changes, and its declared red (R10) is
		// untouched by this edit. ⟦was: AmendPerson(... PadOriented: "true")⟧

		// --- the fixture asserts its own arrangement (rule 6) ------------
		// Without this, the EXACT-SET assertion below certifies nothing
		// about a LATER ADDITION: every candidate part is nil on a plainly
		// created person, so every omitempty part-bearing field a builder
		// might add is invisible by fixture accident rather than by
		// construction.
		live := w3Person(t, st, p.PersonID)
		for _, part := range []struct {
			key string
			got *string
		}{
			{"last_name", live.LastName}, {"first_name", live.FirstName},
			{"position", live.Position}, {"phone", live.Phone},
		} {
			if part.got == nil || *part.got == "" {
				t.Fatalf("PRECONDITION (G-2): the fixture person must CARRY %q, or a payload field added later "+
					"and populated from it is dropped by `omitempty` and this EXACT-SET assertion goes green "+
					"over the addition it exists to catch. [[D-60]]'s forbidden parts are "+
					"last_name/first_name/position/phone (pad_oriented struck at SchemaVersion 11) and the person must carry ALL of them",
					part.key)
			}
		}
		w6CheckInUnsolicited(t, st, "site-a", 1, []string{p.PersonID})

		vs := w6ArrivalVisitPayloads(t, st, "site-a", 1)
		if len(vs) != 1 {
			t.Fatalf("PRECONDITION: the fixture authored %d ArrivalVisit objects, want exactly 1", len(vs))
		}
		// The precondition that makes the exact-set claim meaningful: every
		// omitempty sibling must actually be present here, or "exactly seven"
		// is being asserted of a payload that could never carry seven.
		for _, k := range []string{"company_id", "company_name", "workgroup_id", "workgroup_name"} {
			if _, ok := vs[0][k]; !ok {
				t.Fatalf("PRECONDITION: fixture A must populate every omitempty sibling; %q is absent, so this "+
					"fixture cannot certify the EXACT set", k)
			}
		}
		got := w6SortedKeys(vs[0])
		want := append([]string(nil), permitted...)
		sort.Strings(want)
		if !w6Equal(got, want) {
			t.Errorf("W6-7(b) / W6-6(a) (m6-7): the authored ArrivalVisit marshalled %s, want exactly %s.\n"+
				"  [[D-60]] freezes the DISPLAY NAME and nothing else: first_name, last_name, position, phone, "+
				"pad_oriented and any person_snapshot_* variant are absent BY CONSTRUCTION.\n"+
				"  A missing %q here is the pre-chunk state (the payload marshals six keys at f33c4c7) or, after "+
				"the build, a populate that never set it. An EXTRA key is (m6-7).\n"+
				"  ⚠ GHOST RULE (DR-W6-4): %q must NOT appear as a payload key — the payload spelling is %q.",
				w6Fmt(got), w6Fmt(want), w6ArrivalNameKey, w6GhostPayloadKey, w6ArrivalNameKey)
		}
	})

	// Fixture B — the omitempty absences exercised, so the assertion is of
	// the PERMITTED set and not of one payload's accident.
	t.Run("omitempty-absences", func(t *testing.T) {
		st := w6NewStore(t)
		p, err := CreatePerson(st, "site-a", 1, "Solo, Sam", nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		w6CheckInUnsolicited(t, st, "site-a", 1, []string{p.PersonID})

		vs := w6ArrivalVisitPayloads(t, st, "site-a", 1)
		if len(vs) != 1 {
			t.Fatalf("PRECONDITION: the fixture authored %d ArrivalVisit objects, want exactly 1", len(vs))
		}
		for _, k := range []string{"company_id", "company_name", "workgroup_id", "workgroup_name"} {
			if _, ok := vs[0][k]; ok {
				t.Fatalf("PRECONDITION: fixture B exists to exercise the omitempty ABSENCES; %q is present, "+
					"so it is a duplicate of fixture A and certifies nothing extra", k)
			}
		}
		for _, k := range w6SortedKeys(vs[0]) {
			if !inPermitted(k) {
				t.Errorf("W6-7(b) (m6-7): the ArrivalVisit payload carries %q, which is outside the permitted "+
					"seven %s — [[D-60]]'s violation", k, w6Fmt(permitted))
			}
		}
		if _, ok := vs[0][w6ArrivalNameKey]; !ok {
			t.Errorf("W6-7(b) / W6-1(b): a person with NO company and NO workgroup still has a NAME, and the "+
				"payload carries no %q key. %q is omitempty, so it is absent only when the roster could not "+
				"name the person — which is not this fixture.", w6ArrivalNameKey, w6ArrivalNameKey)
		}
	})
}

// --- ⟦W6 / F-1⟧ THE SEAM-3 PIN, MADE LOUD WHEN IT GOES -------------------
//
// W6's whole contribution to `w5cPinnedOutcome` is ONE line, and it lives in a
// W5c-NAMED file (`w5c_no_consultation_test.go:569`):
//
//	"checkin.visit.person_snapshot_name": "Aurora, Ada"
//
// The build critique DELETED that line and nothing red — `reports/W6-BUILD-CRITIQUE.md`
// F-1, the fifth independent confirmation of G-10. The reason is one line below the
// map: `w5c_no_consultation_test.go:704` reports observed-but-unpinned paths with
// `t.Logf`. A DROPPED key is not an ABSENT key; it is a key that quietly stops being
// checked, and the suite goes green either way.
//
// ⚠ PROMOTING THAT `t.Logf` TO `t.Errorf` IS NOT AVAILABLE AND IS NOT ATTEMPTED HERE.
// The critique measured it: the test logs four observed-but-unpinned paths today, so
// the promotion reds immediately on four paths that have nothing to do with W6. Pinning
// those four is a decision about a LOCKED W5c criterion — `w5cPinnedOutcome`'s own
// FLOOR-NOT-CEILING note, which exists so a parallel sub-chunk adding a field cannot red
// W5c — not a build-time repair. That note is left exactly as it stands.
//
// ⚠ AND THE FLOOR-NOT-CEILING NOTE IS NOT WEAKENED BY WHAT IS BELOW. This guard asserts
// nothing about the key set as a whole. It asserts one thing only: every column of the
// `visit` PROJECTION is pinned to a literal. Paths outside `checkin.visit.` stay
// compared-but-unpinned exactly as W5c rules them, and no existing assertion in that
// file is altered, weakened or removed — this is purely additive, which is the same
// property §1.1 of the critique relies on when it carries the MUTANT row forward.
//
// ⚠ IT IS DERIVED, NEVER HAND-KEPT. A second literal list of eleven column names would
// be the enumeration class this project has now measured going short FIVE times, and it
// would go short in the one place where being short is silent. The `checkin.visit.*`
// paths are produced by `json.Marshal` of a `domain.Visit`
// (`w5c_no_consultation_test.go:180` -> `w5cFlattenInto` -> `w5cWalk`), so the `Visit`
// struct's own json tags ARE the observed key set, and this reads them with `reflect`.
// Consequences, both intended:
//
//   - delete the pin and this test REDS, naming the column and the file;
//   - add a TWELFTH `visit` column and this test REDS until it is pinned too — which is
//     the cover the critique says the map exists to give: `W6-1(b)` covers
//     `person_snapshot_name` specifically, and the map is what covers the NEXT column.
//
// `omitempty` tags are excluded from the required set deliberately: such a field is
// absent from the marshalled bytes when empty, and `w5c_no_consultation_test.go:697`
// reds a pinned path that is ABSENT from the observed outcome. Requiring a pin for one
// would hand a future author a guaranteed red. `Visit` carries no `omitempty` today, so
// the required set is every field it has.
func TestW6ThePinnedVisitColumnsAreAllPinnedAsLiterals(t *testing.T) {
	const prefix = "checkin.visit."

	// (1) the required set, read off the projection struct itself.
	var required []string
	rt := reflect.TypeOf(Visit{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("FIXTURE DEFECT (W6/F-1): Visit.%s carries no json tag, so it never reaches the "+
				"observed path set this guard reads. The guard has gone blind rather than the pin having gone.", f.Name)
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		omitempty := false
		for _, o := range parts[1:] {
			if o == "omitempty" {
				omitempty = true
			}
		}
		if omitempty {
			continue
		}
		required = append(required, name)
	}
	if len(required) == 0 {
		t.Fatalf("FIXTURE DEFECT (W6/F-1): no column was derived from domain.Visit; this guard would pass vacuously")
	}

	// (2) what the pin table actually holds under that prefix, reduced to first
	// segments so a future nested or repeated field ("visit.x.y", "visit.x[0].y")
	// still counts as covering "x".
	pinned := map[string]bool{}
	for p := range w5cPinnedOutcome {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		seg := p[len(prefix):]
		if i := strings.IndexAny(seg, ".["); i >= 0 {
			seg = seg[:i]
		}
		pinned[seg] = true
	}

	// (3) forward — every column must be pinned. THIS is what fires when the
	// SEAM-3 line is dropped.
	for _, c := range required {
		if !pinned[c] {
			t.Errorf("W6/F-1 (G-10): the `visit` column %q is NOT pinned to a literal in w5cPinnedOutcome "+
				"(internal/domain/w5c_no_consultation_test.go). W5c-2 compares unpinned paths between its two runs "+
				"but reports them with t.Logf, so removing a pin removes a CHECK and reds NOTHING — the loss this "+
				"test exists to make loud. Restore the line \"%s%s\": <literal>, or, if the column was deliberately "+
				"removed, remove it from domain.Visit in the same edit.", c, prefix, c)
		}
	}

	// (4) reverse — a pin under this prefix that names no column is a pin gone
	// stale, which passes silently for the same reason.
	for seg := range pinned {
		found := false
		for _, c := range required {
			if c == seg {
				found = true
			}
		}
		if !found {
			t.Errorf("W6/F-1: w5cPinnedOutcome pins %q, which is not a json tag on domain.Visit. Either the column "+
				"was renamed and the pin was left behind — in which case it pins nothing and W5c-2's absent-path "+
				"check is what reds — or the pin was always misspelled and has never been compared.", prefix+seg)
		}
	}
}
