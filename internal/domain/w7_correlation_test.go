// w7_correlation_test.go — W7-2, the criterion the plan itself singles out
// as needing extra verification ("The two-key correlation change gets its own
// verification pass", plan:7038) and the one criterion in the plan's own list
// with NO named mutant. Five rivals are minted against it.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// ⚠⚠ THE ONE DESIGN DECISION IN THIS FILE, stated first because every
// assertion rests on it.
//
// W7 is greenfield in its own artefacts (§2.1: `vehicle_created`,
// `VehicleCreated`, `trip.vehicle_id` — zero occurrences anywhere under
// `app/`). A test that names an unbuilt Go symbol does not fail, it fails to
// COMPILE, and one compile error in `package domain` buries every other W7
// red behind a single message — and takes `internal/domain` down for the
// W9a/W9b/W9c passes whose builds are in this working tree uncommitted.
//
// So this file drives the correlation table THROUGH THE WIRE: every fixture
// is a hand-built envelope carrying §7's FROZEN PAYLOAD KEYS as literals, and
// every observation is read back through `domain.ArrivalStates` — both of
// which exist today. Consequences, all wanted:
//
//   - the file COMPILES AND RUNS at e9cc5f5, so its failures are PRODUCED
//     RUNTIME REDS naming their row of W7-2(a)'s table;
//   - §7 row 2's own accretion is asserted by construction — `vehicle_id` is
//     a payload key on `trip_dispatched` and `trip_arrival`, "the same
//     spelling on both sides, deliberately";
//   - the fold's own rule is honoured: THE FOLD NEVER REFUSES (W7-17,
//     W3-2's rule), so a wire event no authoring edge would mint is exactly
//     the right instrument for rows the edge refuses.
//
// ⚠⚠ AND THE ABSENT-VEHICLE SPELLING IS **OMISSION**, NOT `null` — §7 row 2b
// ⟦LOCK — F7a⟧ settles it: `,omitempty` on a `*string` omits the key and
// never emits `null`, and the four landed marshallers are uniform on it.
// THIS MATTERS FOR THIS FILE SPECIFICALLY: the fold record measured that a
// fixture seeding `vehicle_entered` as a raw wire string literal "compiles
// identically before and after and CHANGES MEANING AT RUN TIME" once
// `DR-W7-4` makes the field a `*string`. The rows below that need an absent
// vehicle therefore OMIT THE KEY rather than writing `""` or `null`, so the
// fixture means the same thing on both sides of `DR-W7-4`.
//
// EXPECTED RED at e9cc5f5 (runtime), by row of W7-2(a):
//   - row (0)   PASSES — the commonest arrival in the system; its job is to
//     kill (m7-2e), the un-deduped union.
//   - row (i)   RED: the id key is not consulted, the strings differ under
//     NormalizeVehicleID, so the answer is `plain` not `correlated`.
//   - row (ii)  PASSES — the string key already carries it. Its job is to kill
//     (m7-2b), §5.7's named regression.
//   - row (iii) RED: with no id key the answer is `correlated`, not
//     `ambiguous`, and no candidate list is pinned.
//   - row (iv)  PASSES — declared NON-DISCRIMINATING across the three rivals.
//   - row (v)   PASSES — declared NON-DISCRIMINATING.
//   - row (vi)  RED: the origin-scoped id key is unbuilt, so an arrival whose
//     strings match nothing lands `plain`.
//   - row (vii) RED (STAGE 2 shape, reachable through the wire).
//   - row (viii)⭐ RED, AND IT IS THE SHARP ONE: the empty vehicle key is
//     TREATED AS A MATCHING VALUE TODAY, so the fold binds THE
//     WRONG CREW TO THE WRONG RUN. (m7-2g) arm A is LIVE.
//   - row (ix)  PASSES — `cancelled_match`, the fifth disposition, unchanged.
//   - row (x)   RED: `a.vehicle == nil` short-circuits BEFORE the id key.
//
// KILLER MUTANTS (§5), each killed by a named row:
//
//	(m7-2a) the id key ignored, string only          -> row (i)
//	(m7-2b) the string key dropped, id only          -> row (ii)
//	(m7-2c) a disagreement auto-picks the id         -> row (iii)
//	(m7-2e) the candidate sets UNIONED WITHOUT DEDUPE-> row (0)
//	(m7-2f) the id key NOT origin-scoped             -> row (vi)
//	(m7-2g) the EMPTY vehicle key treated as a value -> rows (vii)(viii)(ix)(x)
//	(m7-11) NormalizeVehicleID widened into the fold key
//	                                                 -> the pinned literals below
package domain

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- §7's frozen wire literals, as constants -----------------------------
//
// ⚠ These are B-2 freeze-list literals, not a free choice (§7 rows 1, 2, 2b).
// Row 2 is WIDENED: `vehicle_id` is a payload key on `trip_dispatched` and
// `trip_arrival` — "the two EXISTING wire shapes item 9 changes — and those
// bytes are inside `Hash()`ed envelopes, which is what B-2 exists to freeze."
const (
	w7KeyVehicleID      = "vehicle_id"
	w7KeyVehicleEntered = "vehicle_entered"
	w7EvtTripDispatched = "trip_dispatched"
	w7EvtTripArrival    = "trip_arrival"
)

// w7Dispositions is §7 row 10's UNCHANGED-SET assertion, PINNED AS LITERALS
// and never as an umbrella term: "the five existing values" is exactly the
// category-not-literal row B-2's freeze discipline forbids.
var w7Dispositions = []string{
	"expected", "correlated", "plain", "ambiguous", "cancelled_match",
}

// --- wire machinery (NO assertions) --------------------------------------

// w7Wire hand-builds one envelope from a raw payload map. W7 mints its own
// rather than calling W5d's: W7-X3 forbids another chunk's green counting
// toward W7's, and a helper shared across chunk boundaries is a dependency
// that can be deleted out from under it.
//
// ⚠ The payload is a MAP, so a key is ABSENT when it is not set — never `""`
// and never `null` (§7 row 2b's OMISSION spelling).
func w7Wire(t *testing.T, siteID string, epoch, seq, atMS int64, payload map[string]any) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: atMS, TimeSource: "default",
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = atMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

// w7Dispatch builds a `trip_dispatched` envelope. `vehicleEntered == nil`
// OMITS the key (the absent-number shape); `vehicleID == nil` omits the id.
func w7Dispatch(t *testing.T, originSite string, epoch, seq, atMS int64,
	tripID string, vehicleID, vehicleEntered *string, dest string) store.Envelope {
	t.Helper()
	p := map[string]any{
		"type":                w7EvtTripDispatched,
		"trip_id":             tripID,
		"driver_person_id":    "p-driver",
		"occupant_person_ids": []string{"p-driver"},
		"destination_site":    dest,
		"deadline_minutes":    int64(120),
	}
	if vehicleEntered != nil {
		p[w7KeyVehicleEntered] = *vehicleEntered
	}
	if vehicleID != nil {
		p[w7KeyVehicleID] = *vehicleID
	}
	return w7Wire(t, originSite, epoch, seq, atMS, p)
}

// w7Arrival builds an UNSOLICITED `trip_arrival` envelope (TripRef nil — the
// §9.2 shape the correlation rule table is about).
func w7Arrival(t *testing.T, atSite string, epoch, seq, atMS int64,
	arrivalID string, vehicleID, vehicleEntered *string, originClaim *string) store.Envelope {
	t.Helper()
	p := map[string]any{
		"type":       w7EvtTripArrival,
		"arrival_id": arrivalID,
		"visits":     []map[string]any{{"visit_id": "vis-" + arrivalID, "person_id": "p-driver"}},
	}
	if vehicleEntered != nil {
		p[w7KeyVehicleEntered] = *vehicleEntered
	}
	if vehicleID != nil {
		p[w7KeyVehicleID] = *vehicleID
	}
	if originClaim != nil {
		p["origin_site"] = *originClaim
	}
	return w7Wire(t, atSite, epoch, seq, atMS, p)
}

func w7s(s string) *string { return &s }

// w7World opens a registered store and applies a stream. Applying through
// `Apply` is deliberate: it is the FOLD, and W7-17 (W3-2's rule) says the
// fold NEVER refuses — so an event no authoring edge would mint still lands.
func w7World(t *testing.T, name string, stream ...store.Envelope) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := Register(st); err != nil {
		t.Fatal(err)
	}
	for i, ev := range stream {
		if _, err := st.Apply(ev); err != nil {
			t.Fatalf("W7-17 / W3-2's rule: THE FOLD NEVER REFUSES, and it refused envelope %d (%s/%d/%d): %v",
				i, ev.SiteID, ev.NodeEpoch, ev.Seq, err)
		}
	}
	return st
}

// w7Disposition reads back one arrival's disposition, bound trip and
// candidate list.
func w7Disposition(t *testing.T, st *store.Store, arrivalID string) (disp string, tripID string, candidates string) {
	t.Helper()
	states, err := ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.ArrivalID != arrivalID {
			continue
		}
		if s.TripID != nil {
			tripID = *s.TripID
		}
		if s.Candidates != nil {
			candidates = *s.Candidates
		}
		return s.Disposition, tripID, candidates
	}
	t.Fatalf("W7-2 FIXTURE DEFECTIVE: no arrival_state row for %q — the arrival did not fold, so every "+
		"assertion about its disposition would be vacuous", arrivalID)
	return "", "", ""
}

// =========================================================================
// W7-2(a) — THE RULE, as a fixture table with every cell a pinned literal
// =========================================================================
//
// Cloned from the spec of record's own rule table (analytics-v1.md:880-886)
// and turned into a fixture in which THE CERTIFIED RULE AND EVERY NAMED RIVAL
// RETURN DIFFERENT ANSWERS (standing rule 3) — except on the two rows the
// criterion itself declares NON-DISCRIMINATING, which are kept with their job
// stated rather than dropped.
//
// ⚠ Rows (iv) and (v) are DEGENERATE ACROSS THE THREE RIVALS and are kept
// anyway: (iv) pins that the id key ADDS no false positive, and (v) pins that
// "≥2 candidates by either → ambiguous" is UNCHANGED. **They are not evidence
// for the certified rule and are not booked as killing any mutant.** Saying so
// is the difference between a row that does a job and a row that looks like
// coverage.
func TestW7CorrelationRuleTable(t *testing.T) {
	const (
		ice = "ice-road"
		pad = "pad"
	)

	type row struct {
		name string
		// what the row is FOR, so a green is not mistaken for evidence
		kills string
		// the stream, built per row
		stream func(t *testing.T) []store.Envelope
		// the arrival under test
		arrivalID string
		// CERTIFIED, pinned as literals
		wantDisposition string
		wantTrip        string // "" = bound to nothing
		wantCandidates  string // "" = not asserted (only ambiguous rows pin it)
		// the rivals' answers, pinned so the fixture's non-degeneracy is
		// asserted here rather than argued in prose
		rivalA string // (m7-2a) id ignored, string only
		rivalB string // (m7-2b) id only, string dropped
		rivalC string // (m7-2c) disagreement auto-picks the id
	}

	rows := []row{
		{
			name:  "row (0) BOTH KEYS HIT THE SAME TRIP — the case EVERY NORMAL ARRIVAL TAKES",
			kills: "(m7-2e) — the two candidate sets UNIONED WITHOUT DEDUPE, so the trip is counted twice and `≥2 candidates` fires",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-0", w7s("v-1"), w7s("TRK-14"), w7s(ice)),
				}
			},
			arrivalID: "a-0", wantDisposition: "correlated", wantTrip: "t-1",
			rivalA: "correlated", rivalB: "correlated", rivalC: "correlated",
		},
		{
			name:  "row (i) THE ID AGREES AND THE STRINGS DIFFER",
			kills: "(m7-2a) — the id key ignored; the strings differ under NormalizeVehicleID, which PRESERVES PUNCTUATION",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("607-E0245"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-i", w7s("v-1"), w7s("607e0245"), w7s(ice)),
				}
			},
			arrivalID: "a-i", wantDisposition: "correlated", wantTrip: "t-1",
			rivalA: "plain", rivalB: "correlated", rivalC: "correlated",
		},
		{
			name:  "row (ii) THE PARTITIONED ARRIVAL — this post never heard of the truck",
			kills: "(m7-2b) — the string key dropped. §5.7's named regression: the partitioned arrival cannot match and the trip STAYS OPEN FOREVER",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-ii", nil, w7s("TRK-14"), w7s(ice)),
				}
			},
			arrivalID: "a-ii", wantDisposition: "correlated", wantTrip: "t-1",
			rivalA: "correlated", rivalB: "plain", rivalC: "correlated",
		},
		{
			name:  "row (iii) THE KEYS DISAGREE — bind NOTHING, review lane",
			kills: "(m7-2c) — a disagreement auto-picks the id candidate. \"never auto-pick, a wrong match would silence a real alert\"",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-a", w7s("v-1"), w7s("TRK-14"), pad),
					w7Dispatch(t, ice, 1, 2, 1_100, "t-b", w7s("v-2"), w7s("TRK-9"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-iii", w7s("v-2"), w7s("TRK-14"), w7s(ice)),
				}
			},
			arrivalID: "a-iii", wantDisposition: "ambiguous", wantTrip: "",
			// ⚠⚠ ⟦LOCK — FIX-F16 (F1b)⟧ THE TRIPS ARE NAMED SO THAT LEXICAL
			// ORDER AND SOURCE-KEY ORDER DIFFER, AND THE LITERAL IS PINNED.
			// `t-a` is the STRING-key candidate and `t-b` is the ID-key one;
			// the certified list is `t-b,t-a` — id key FIRST, per this
			// criterion's own headline rule — while the LEXICAL order is
			// `t-a,t-b`. A build that sorts the candidate list alphabetically
			// FAILS on this literal and would have passed under the
			// superseded `A`/`B` naming, where the two orders coincided by
			// construction.
			wantCandidates: "t-b,t-a",
			rivalA:         "correlated", rivalB: "correlated", rivalC: "correlated",
		},
		{
			name:  "row (iv) NO OPEN TRIP MATCHES EITHER KEY — DECLARED NON-DISCRIMINATING",
			kills: "NOTHING. It pins that the id key ADDS NO FALSE POSITIVE. It is not evidence for the certified rule and is not booked as killing any mutant.",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-iv", w7s("v-9"), w7s("TRK-99"), w7s(ice)),
				}
			},
			arrivalID: "a-iv", wantDisposition: "plain", wantTrip: "",
			rivalA: "plain", rivalB: "plain", rivalC: "plain",
		},
		{
			name:  "row (v) TWO OPEN TRIPS ON ONE VEHICLE, ONE ORIGIN — DECLARED NON-DISCRIMINATING",
			kills: "NOTHING. It pins that \"≥2 candidates by either → ambiguous\" is UNCHANGED (analytics-v1.md:885, \"unchanged (trip.go:838-840)\").",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Dispatch(t, ice, 1, 2, 1_100, "t-2", w7s("v-1"), w7s("TRK-14"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-v", w7s("v-1"), w7s("TRK-14"), w7s(ice)),
				}
			},
			arrivalID: "a-v", wantDisposition: "ambiguous", wantTrip: "",
			wantCandidates: "t-1,t-2",
			rivalA:         "ambiguous", rivalB: "ambiguous", rivalC: "ambiguous",
		},
		{
			name: "row (vi) ONE TRUCK, TWO OPEN RUNS, TWO ORIGINS — the id key is ORIGIN-SCOPED",
			kills: "(m7-2f) — the id key NOT origin-scoped. A truck that ran Ice Road→Pad in the morning and " +
				"Pad→Ice Road in the afternoon has two open trips; an unscoped id key lets an arrival claiming " +
				"one origin bind to the run from the other — a WRONG-CREW BINDING, priced as \"an accountability " +
				"defect under [[D-6]], not a statistics annoyance\".",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					// ⚠ The two trips carry DIFFERENT entered strings and the
					// SAME vehicle_id, and the arrival's string matches
					// NEITHER — so the row is decided by the ID key alone and
					// the string key cannot mask a wrong answer.
					w7Dispatch(t, ice, 1, 1, 1_000, "t-A", w7s("v-1"), w7s("TRK-A"), pad),
					w7Dispatch(t, pad, 1, 1, 1_100, "t-B", w7s("v-1"), w7s("TRK-B"), ice),
					w7Arrival(t, pad, 2, 1, 2_000, "a-vi", w7s("v-1"), w7s("TRK-Z"), w7s(ice)),
				}
			},
			arrivalID: "a-vi", wantDisposition: "correlated", wantTrip: "t-A",
			// rival D `(m7-2f)`: the id key NOT origin-scoped → ambiguous,
			// binding nothing.
			rivalA: "plain", rivalB: "correlated", rivalC: "correlated",
		},
		{
			name: "row (vii) STAGE 2 — TWO NUMBERLESS TRIPS, the id carries it",
			kills: "(m7-2g) arm A — the empty/absent string key treated as a MATCHING VALUE, which makes both " +
				"numberless trips string-key candidates and the answer `ambiguous`.",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-7", w7s("v-7"), nil, pad),
					w7Dispatch(t, ice, 1, 2, 1_100, "t-8", w7s("v-8"), nil, pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-vii", w7s("v-7"), nil, w7s(ice)),
				}
			},
			arrivalID: "a-vii", wantDisposition: "correlated", wantTrip: "t-7",
			rivalA: "ambiguous", rivalB: "correlated", rivalC: "correlated",
		},
		{
			name: "row (viii) ⭐ STAGE 2, THE OPERATOR SCENARIO — a DIFFERENT truck, and NOTHING BINDS",
			kills: "(m7-2g) arm A, on the sharp side: under the rival this is `correlated` → THE WRONG CREW " +
				"BOUND TO THE WRONG RUN, SILENTLY.",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-7", w7s("v-7"), nil, pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-viii", w7s("v-9"), nil, w7s(ice)),
				}
			},
			arrivalID: "a-viii", wantDisposition: "plain", wantTrip: "",
			rivalA: "correlated", rivalB: "plain", rivalC: "plain",
		},
		{
			name:  "row (ix) THE CANCELLED MATCH — the fifth disposition, unchanged",
			kills: "(m7-2g) arm C — `cancelled_match` bypassed by a two-key candidacy. DR-W7-3: W7 adds NO sixth disposition.",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Wire(t, ice, 1, 2, 1_100, map[string]any{
						"type": "trip_cancelled", "trip_id": "t-1", "reason": "stood down",
					}),
					w7Arrival(t, pad, 2, 1, 2_000, "a-ix", w7s("v-1"), w7s("TRK-14"), w7s(ice)),
				}
			},
			arrivalID: "a-ix", wantDisposition: "cancelled_match", wantTrip: "",
			rivalA: "cancelled_match", rivalB: "cancelled_match", rivalC: "cancelled_match",
		},
		{
			name: "row (x) THE STRUCTURAL SHORT-CIRCUIT — the id key MUST be consulted",
			kills: "(m7-2g) arm B — the `a.vehicle == nil` short-circuit LEFT AS IS, so the arrival falls to " +
				"`plain` BEFORE the id key is ever read. \"That is the case the id key exists for, and after " +
				"item 8 it is the NORMAL shape for a role-only vehicle.\"",
			stream: func(t *testing.T) []store.Envelope {
				return []store.Envelope{
					w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
					w7Arrival(t, pad, 2, 1, 2_000, "a-x", w7s("v-1"), nil, w7s(ice)),
				}
			},
			arrivalID: "a-x", wantDisposition: "correlated", wantTrip: "t-1",
			rivalA: "plain", rivalB: "correlated", rivalC: "correlated",
		},
	}

	// --- FIXTURE NON-DEGENERACY, ASSERTED AT RUN TIME rather than argued in
	// prose. Standing rule 3: a criterion certifying WHICH rule is in force
	// needs a fixture where the certified rule and EVERY NAMED RIVAL return
	// different answers, pinned as a literal. The two rows the criterion
	// declares non-discriminating are exempted BY NAME, not by silence.
	nonDiscriminating := map[string]bool{
		"row (iv) NO OPEN TRIP MATCHES EITHER KEY — DECLARED NON-DISCRIMINATING":          true,
		"row (v) TWO OPEN TRIPS ON ONE VEHICLE, ONE ORIGIN — DECLARED NON-DISCRIMINATING": true,
		"row (ix) THE CANCELLED MATCH — the fifth disposition, unchanged":                 true,
		"row (0) BOTH KEYS HIT THE SAME TRIP — the case EVERY NORMAL ARRIVAL TAKES":       true,
	}
	discriminated := 0
	for _, r := range rows {
		differs := r.rivalA != r.wantDisposition || r.rivalB != r.wantDisposition || r.rivalC != r.wantDisposition
		if nonDiscriminating[r.name] {
			continue
		}
		if !differs {
			t.Errorf("W7-2(a) FIXTURE DEFECT on %s: the certified answer %q equals every rival's, so the row "+
				"certifies nothing and is not marked non-discriminating. A fixture whose certified rule and "+
				"named rivals agree \"certifies nothing\" (standing rule 3).", r.name, r.wantDisposition)
			continue
		}
		discriminated++
	}
	if discriminated == 0 {
		t.Fatal("W7-2(a) FIXTURE DEFECT: not one row separates the certified rule from a rival — the whole " +
			"table would be a description of the build rather than a check on it")
	}

	// --- THE ASSERTION, row by row ---------------------------------------
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			st := w7World(t, "w7-corr.db", r.stream(t)...)
			disp, trip, cands := w7Disposition(t, st, r.arrivalID)

			if disp != r.wantDisposition {
				t.Errorf("W7-2(a) %s\n  disposition = %q, want %q\n  KILLS: %s",
					r.name, disp, r.wantDisposition, r.kills)
			}
			if trip != r.wantTrip {
				t.Errorf("W7-2(a) %s\n  bound trip = %q, want %q — ⚠ an `ambiguous` arrival BINDS NOTHING",
					r.name, trip, r.wantTrip)
			}
			if r.wantCandidates != "" && cands != r.wantCandidates {
				t.Errorf("W7-2(a) %s\n  candidate list = %q, want %q (pinned as a LITERAL in SOURCE-KEY order — "+
					"the id key is consulted first, so its candidate is appended first; the LEXICAL order here "+
					"is deliberately DIFFERENT, so a build that sorts the list alphabetically FAILS)\n  KILLS: %s",
					r.name, cands, r.wantCandidates, r.kills)
			}

			// §7 row 10's UNCHANGED-SET assertion, at each row: the answer is
			// one of the FIVE frozen values. ⚠ Asserted as MEMBERSHIP, not as
			// set equality — see the note on this test's sixth-disposition
			// finding in reports/window-build/W7-tests.md §4.
			if !w7IsFrozenDisposition(disp) {
				t.Errorf("§7 row 10 / DR-W7-3: disposition %q is outside the frozen five %v — W7 adds NO sixth "+
					"disposition and `arrival_state.disposition`'s vocabulary is unchanged", disp, w7Dispositions)
			}
		})
	}
}

func w7IsFrozenDisposition(d string) bool {
	for _, v := range w7Dispositions {
		if v == d {
			return true
		}
	}
	return false
}

// =========================================================================
// W7-2(b) — NormalizeVehicleID IS UNCHANGED, pinned as literals
// =========================================================================
//
// ⟦NO PRE-CHUNK RED — §2.4 row 2⟧ `trip.go:86-88` already preserves
// punctuation, so `"TRK-14"` and `"TRK 14"` already fail to correlate. The
// evidence is the WIDENING PLANT — (m7-11) — and never the clean pass.
//
// ⚠ This is not decoration. The natural way to build `vehicleFoldKey` is to
// WIDEN `NormalizeVehicleID`, and that would silently change correlation
// semantics for EVERY TRIP IN THE SYSTEM: `TRK-14` and `TRK 14` would begin to
// correlate, and ROW (i) OF THE TABLE ABOVE WOULD FLIP FROM `plain` TO
// `correlated` UNDER RIVAL A WHILE STILL PASSING.
//
// ⚠ (m7-11) IS ONE PLANT WITH TWO DETECTORS — this behavioural one and
// W7-11(a)/(c)'s structural one — AND IT IS ONE KILL, recorded once
// [DOC — plan:6792, the ledger's CONTROL row: one plant is one piece of
// evidence].
func TestW7NormalizeVehicleIDIsUnchanged(t *testing.T) {
	// The F12 property, whose loss "stayed open forever": NFC normalisation.
	// "TRÜCK-1" in NFC and in NFD are the same glyph and different bytes.
	const nfc = "TRÜCK-1"  // U+00DC LATIN CAPITAL LETTER U WITH DIAERESIS
	const nfd = "TRÜCK-1" // U + COMBINING DIAERESIS

	if got := NormalizeVehicleID(nfc); got != NormalizeVehicleID(nfd) {
		t.Errorf("W7-2(b): NormalizeVehicleID is no longer NFC-normalising — %q vs %q. F12 was added after a "+
			"REPRODUCED correlation break: a trip stored in NFC and an arrival typed in NFD failed to correlate "+
			"and the trip stayed open forever.", got, NormalizeVehicleID(nfd))
	}

	// ⭐ THE PUNCTUATION PROPERTY, which is what separates NormalizeVehicleID
	// from vehicleFoldKey. A fixture using a pair the two functions AGREE on
	// certifies nothing — `"TRUCK 12"` is exactly such a pair and is the
	// string the existing comment uses, so it is the one a builder reaches
	// for. These pairs are chosen because the two functions DISAGREE on them.
	for _, tc := range []struct{ a, b string }{
		{"TRK-14", "TRK 14"},
		{"607-E0245", "607e0245"},
	} {
		if NormalizeVehicleID(tc.a) == NormalizeVehicleID(tc.b) {
			t.Errorf("W7-2(b) / W7-11 (m7-11): NormalizeVehicleID(%q) == NormalizeVehicleID(%q) == %q. "+
				"Punctuation is DELIBERATELY PRESERVED (trip.go:86-88); this is the correlation comparison form, "+
				"and widening it into the fold key silently changes correlation for EVERY TRIP IN THE SYSTEM.",
				tc.a, tc.b, NormalizeVehicleID(tc.a))
		}
	}

	// ⭐ THE NON-DEGENERATE PAIR THE FOLD KEY WOULD COLLAPSE, pinned so the
	// two functions' answers are separated by a LITERAL: `vehicleFoldKey`
	// strips every non-alphanumeric rune, so both of these fold to
	// `607e0245`. NormalizeVehicleID must not.
	if got := NormalizeVehicleID("607-E0245"); got == "607e0245" {
		t.Errorf("W7-11(c) (m7-11): NormalizeVehicleID(%q) = %q — that is the FOLD KEY's answer. The two are "+
			"TWO FUNCTIONS and neither becomes the other: the fold key is for search ranking and the guard-2 "+
			"prompt, \"and for nothing else. It never merges, never correlates, never reaches a database column.\"",
			"607-E0245", got)
	}

	// The existing behaviour that must survive, pinned so a rewrite is
	// visible: trim, collapse internal whitespace, lower-case.
	for _, tc := range []struct{ in, want string }{
		{"TRUCK 12", "truck 12"},
		{"  TRUCK   12", "truck 12"},
		{"\tTrUcK\t12\n", "truck 12"},
		{"   ", ""},
	} {
		if got := NormalizeVehicleID(tc.in); got != tc.want {
			t.Errorf("W7-2(b): NormalizeVehicleID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// =========================================================================
// W7-2(e) — NO REFUSAL IS REMOVED
// =========================================================================
//
// [DOC — analytics-v1.md:888-891] "The existing discipline is not weakened
// anywhere … The id adds a stronger first key; it removes no refusal."
// Asserted by the fixture set above answering `ambiguous` wherever it answers
// `ambiguous` today — which is rows (iii) and (v), and which this test states
// as a PROPERTY over the table rather than as a second copy of it.
func TestW7TheIDKeyRemovesNoRefusal(t *testing.T) {
	// The property: every arrival that is ambiguous today is still ambiguous
	// under the two-key rule, and no arrival that binds today binds to a
	// DIFFERENT trip.
	//
	// The fixture is row (v)'s — two open trips on one vehicle from one
	// origin — replayed with the id key present on all three envelopes. The
	// id AGREES with the string on both trips, so a build that consults the
	// id cannot use it to break the tie.
	const (
		ice = "ice-road"
		pad = "pad"
	)
	st := w7World(t, "w7-noremove.db",
		w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
		w7Dispatch(t, ice, 1, 2, 1_100, "t-2", w7s("v-1"), w7s("TRK-14"), pad),
		w7Arrival(t, pad, 2, 1, 2_000, "a-e", w7s("v-1"), w7s("TRK-14"), w7s(ice)),
	)
	disp, trip, _ := w7Disposition(t, st, "a-e")
	if disp != "ambiguous" {
		t.Errorf("W7-2(e): two open trips on one vehicle from one origin gave %q, want \"ambiguous\". "+
			"\"The id adds a stronger first key; it REMOVES NO REFUSAL\" (analytics-v1.md:888-891). ⚠ A build "+
			"that uses the id to break a tie the string key could not break has weakened the discipline, and "+
			"\"never auto-pick — a wrong match would silence a real alert\".", disp)
	}
	if trip != "" {
		t.Errorf("W7-2(e): an `ambiguous` arrival bound to %q — it must bind NOTHING and go to the review lane", trip)
	}
}

// =========================================================================
// §7 row 10 — the UNCHANGED-SET assertion, as its own check
// =========================================================================
//
// ⚠⚠ THIS TEST IS DELIBERATELY A MEMBERSHIP CHECK OVER W7'S OWN OUTPUTS AND
// **NOT** A SET-EQUALITY CHECK OVER THE Go CONSTANTS, and the reason is a
// MEASUREMENT that disagrees with the lock. §7 row 10 pins the vocabulary at
// "the FIVE existing values … [MEASURED — trip.go:52-67]". On this working
// tree `trip.go` declares a SIXTH — `ArrivalRefRetracted` ("ref_retracted"),
// added by W9b's uncommitted build ("⟦W9b, item 16⟧ … an EXPLICIT arrival
// whose TripRef selection a human withdrew").
//
// A set-equality assertion would therefore red for a reason that IS NOT
// W7'S — another window's addition — and the ledger would record a W7 failure
// against W9b's work. What W7's criterion actually says is that **W7 adds no
// sixth disposition**, so that is what is asserted: every disposition W7's own
// fixtures produce is inside the frozen five, and no `vehicle`-shaped
// disposition exists at all.
//
// Reported at reports/window-build/W7-tests.md §4 rather than accommodated
// silently.
func TestW7AddsNoArrivalDisposition(t *testing.T) {
	const (
		ice = "ice-road"
		pad = "pad"
	)
	st := w7World(t, "w7-vocab.db",
		w7Dispatch(t, ice, 1, 1, 1_000, "t-1", w7s("v-1"), w7s("TRK-14"), pad),
		w7Arrival(t, pad, 2, 1, 2_000, "a-1", w7s("v-1"), w7s("TRK-14"), w7s(ice)),
		w7Arrival(t, pad, 2, 2, 2_100, "a-2", w7s("v-9"), w7s("TRK-99"), w7s(ice)),
	)
	states, err := ArrivalStates(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		t.Fatal("§7 row 10 FIXTURE DEFECTIVE: no arrival_state rows, so the membership check covers nothing")
	}
	var seen []string
	for _, s := range states {
		if !w7IsFrozenDisposition(s.Disposition) {
			t.Errorf("§7 row 10 / DR-W7-3: arrival %s has disposition %q, outside the frozen five %v. "+
				"W7 adds NO sixth disposition — analytics-v1.md:880-886's rule table rules the id/string "+
				"disagreement `ArrivalAmbiguous`, BY NAME, with no sixth disposition.",
				s.ArrivalID, s.Disposition, w7Dispositions)
		}
		seen = append(seen, s.Disposition)
	}
	sort.Strings(seen)
	if strings.Contains(strings.Join(seen, ","), "vehicle") {
		t.Errorf("§7 row 10: a vehicle-shaped disposition appeared: %v", seen)
	}
}
