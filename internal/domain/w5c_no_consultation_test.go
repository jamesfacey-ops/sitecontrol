// w5c_no_consultation_test.go — W5c-2 (b), THE NON-DEGENERATE FIXTURE —
// CORRECTED TO THE SCHEMA-11 SHAPE.
//
//	W5c-2  ⟦was⟧ No software path consults it — it is a RECORD, NOT A GATE
//	       (plan :2048; D-10).
//	W5c-2  ⟦SchemaVersion 11⟧ No software path consults the STALE KEY. The
//	       fact is struck ([[D-88]] ERRATA / [[D-91]] — and D-88 REVERSES
//	       D-10: per-site orientation WILL gate entry, as the ORIENTING
//	       post's own record, in a later stage of this increment). What can
//	       still reach this post is a divergent peer's person_amended that
//	       carries the old key, and W3-2 obliges the fold to land it. This
//	       fixture proves that landing it changes NOTHING downstream: two
//	       otherwise-identical persons, one whose wire amendment asserts the
//	       stale key and one whose clears it, taken through CHECK-IN and
//	       DISPATCH, with the outcomes asserted IDENTICAL — field by field
//	       against pinned literals, not "no error" and not a smoke
//	       assertion. The discriminator moved from the composed field (gone)
//	       to the RAW LOG BYTES, which is the only place the key survives.
//
// The (a) SCAN half — which is what covers ARRIVAL, MUSTER and ALERT, the
// three families no fixture here drives — lives in
// internal/store/w5c_source_scans_test.go
// (TestW5cNoProductionCodeNamesPadOrientation; ⟦was:
// "TestW5cNoSoftwarePathConsultsPadOrientation"⟧).
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Machinery and seams: w5c_seams_test.go.
//
// ⟦NO PRE-CHUNK RED⟧ — "zero consumers of PadOriented" is the lock's own
// measurement, so THE CLEAN PASS BELOW IS NOT EVIDENCE. The evidence is
// (m5c-2) — a check-in or dispatch path branching on the flag — produced
// against a real on-disk tree, ON THIS FIXTURE. The fixture is what makes
// that mutant killable at all, which is why its non-degeneracy is checked at
// runtime rather than asserted in a comment.
//
// ⚠ MusterLive IS DELIBERATELY NOT CALLED HERE. W5d changes its signature
// (the company merge map) and holds a standing ownership grant to update its
// invocations in test files. A W5c fixture built around it would be rewritten
// under this writer by another sub-chunk's builder. The muster family is
// covered by the scan half instead — which is the half the lock added for
// exactly the families no fixture drives.
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the scenario -------------------------------------------------------

const (
	w5cSite        = "site-a"
	w5cPeer        = "site-b"
	w5cEpoch       = int64(1)
	w5cVehicle     = "TRK-118"
	w5cCompanyName = "Kakivik Asset Management"
	// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "w5cWorkgroupName = \"Crew One\""⟧
	// BAR 3: the NAME is unchanged and that is the point — "Crew One" is a
	// PINNED LITERAL. Only the entity it names moved (W8's Workgroup→Trade).
	w5cTradeName    = "Crew One"
	w5cSubjectName  = "Aurora, Ada"
	w5cOfficer      = "Officer Vega"
	w5cDeadlineMins = int64(90)
)

// w5cRun is one subject's whole passage through the system.
type w5cRun struct {
	Oriented bool
	Store    *store.Store
	SubjectID,
	CompanyID,
	// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "	WorkgroupID,"⟧ BAR 3: W8's rename.
	TradeID,
	// ⟦LOCK — §57, 2026-09-03⟧ VehicleID is NEW: BAR 1's normalisation needs
	// the run's own vehicle record id, and carrying it here is also what makes
	// "both acts name the SAME truck" a single assignment rather than a claim.
	VehicleID,
	CheckInTripID,
	DispatchTripID,
	ArrivalID,
	VisitID string
	// Outcome is the flattened, id-normalised full result: "path" -> "value".
	Outcome map[string]string
	// Subject is the composed person as READ BACK. Since SchemaVersion 11 it
	// is NOT the discriminator — the struck fact composes to nothing on both
	// runs, which is part of what this fixture proves.
	Subject *Person
	// StalePad is the discriminator: the stale `pad_oriented` value the
	// run's wire amendment carried, read back off the LOG BYTES (nil when the
	// amendment cleared rather than asserted it). The RECORD differs here and
	// only here; the OUTCOMES must not.
	StalePad *string
}

func w5cSeedCompany(t *testing.T, st *store.Store, seq int64, id, name string) {
	t.Helper()
	env := w5cEnvelope(t, w5cPeer, 7, seq, w5cBase, map[string]any{
		"type": "company_created", "company_id": id, "name": name,
	})
	w5cApply(t, st, env, "fixture")
}

// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "func w5cSeedWorkgroup(t *testing.T, st *store.Store, seq int64, id, companyID, name string) {"⟧
// ⟦was: "		\"type\": \"workgroup_created\", \"workgroup_id\": id, \"company_id\": companyID, \"name\": name,"⟧
//
// ⚠ BAR 3 — THE FIXTURE WAS SEEDING AN EVENT TYPE NOTHING FOLDS. W8 renamed
// the entity and `workgroup_created` has no projection any more (grep the
// non-test tree: zero hits). The envelope was still authored and still
// applied, so nothing failed — the roster row simply carried no group, and
// `visit.trade_snapshot_*` composed to NULL while four pinned paths quietly
// stopped existing. ⚠ THE TEST STILL COMPILED AND READ AS COVERAGE.
//
// ⛔ §57 BARS the tempting repair — re-pointing those pins at
// `trade_snapshot_*` would pin them to `null`, lose the "Crew One" assertion
// outright, AND GO GREEN. The fixture is what must become true again, so this
// seeds a TRADE through the live fold path (`applyTradeCreated`, refdata.go:
// 353), exactly as w5cSeedCompany already does for `company_created`.
//
// The trade carries no company: `TradeCreated` has no company_id key
// (refdata.go:53-58), which is the rename's own shape and not an omission
// here. companyID is therefore no longer a parameter.
func w5cSeedTrade(t *testing.T, st *store.Store, seq int64, id, name string) {
	t.Helper()
	env := w5cEnvelope(t, w5cPeer, 7, seq, w5cBase, map[string]any{
		"type": "trade_created", "trade_id": id, "name": name,
	})
	w5cApply(t, st, env, "fixture")
}

// w5cOrientOverTheWire gives the subject its orientation — or, in the plain
// run, its EXPLICIT non-orientation — through a peer's person_amended
// envelope rather than through this post's own authoring edge.
//
// ⚠ TWO REASONS, both measured, and neither is a shortcut.
//
//	(1) AUTHORING LOCALLY MADE THE FIXTURE DEGENERATE IN A WAY THAT LOOKED
//	    LIKE A CONSULTATION. The oriented run appended one extra event to
//	    THIS post's stream, so every downstream `seq` in the outcome shifted
//	    by one and the comparison reported "pad orientation changed the
//	    outcome at checkin.trip.seq" against a tree that consults nothing
//	    [MEASURED — it did, on the first run]. `seq` is not a field to
//	    normalise away: an extra event authored IS one of the shapes a real
//	    consultation would take.
//	(2) IT KEEPS THE FIXTURE INDEPENDENT OF W5c's OWN EDGE GUARD. W5c is
//	    about to add a value-domain refusal to AmendPerson; a W5c-2 fixture
//	    routed through that refusal would couple this criterion to another
//	    criterion's build.
//
// BOTH runs receive exactly one amendment, at the same key, so they differ
// in the STALE KEY'S BYTES and in nothing else: the "oriented" run's wire
// amendment asserts `pad_oriented`, the plain run's CLEARS it. Since
// SchemaVersion 11 neither composes to anything — there is no field — and
// the fold lands both without refusing (W3-2).
func w5cOrientOverTheWire(t *testing.T, r *w5cRun) {
	t.Helper()
	var fields map[string]any
	var clear []string
	if r.Oriented {
		fields = map[string]any{w5cKeyPadOriented: w5cOriented}
	} else {
		clear = []string{w5cKeyPadOriented}
	}
	env := w5cWirePersonAmended(t, w5cPeer, 7, 4, w5cBase, r.SubjectID, fields, clear)
	w5cApply(t, r.Store, env, "fixture")
}

// w5cBuildRun constructs one subject and takes them through check-in and
// dispatch. The two runs live in SEPARATE STORES, built by this one
// function, so "the outcomes are identical" is a literal field-by-field
// claim rather than an exercise in explaining away interleaved sequence
// numbers. Everything but the flag is byte-identical by construction; the
// self-check below verifies that from READ-BACK DATA rather than trusting it.
func w5cBuildRun(t *testing.T, oriented bool) *w5cRun {
	t.Helper()
	r := &w5cRun{Oriented: oriented, Store: w5cNewStore(t)}
	r.CompanyID = w5cPeer + "-company-0001"
	// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "	r.WorkgroupID = w5cPeer + \"-workgroup-0001\""⟧
	// ⟦was: "	w5cSeedWorkgroup(t, r.Store, 2, r.WorkgroupID, r.CompanyID, w5cWorkgroupName)"⟧
	// ⟦was: "		\"company_id\": r.CompanyID, \"workgroup_id\": r.WorkgroupID,"⟧
	// BAR 3. The id stays FIXTURE-MINTED and deterministic — same shape as the
	// company id above — so it normalises to a stable placeholder exactly as
	// the workgroup id did, and the seq positions on the peer stream are
	// unchanged (one event in, one event out).
	r.TradeID = w5cPeer + "-trade-0001"
	w5cSeedCompany(t, r.Store, 1, r.CompanyID, w5cCompanyName)
	w5cSeedTrade(t, r.Store, 2, r.TradeID, w5cTradeName)
	r.SubjectID = w5cSeedPerson(t, r.Store, 3, w5cSubjectName, map[string]any{
		"company_id": r.CompanyID, "trade_id": r.TradeID,
	})

	w5cOrientOverTheWire(t, r)
	r.Subject = w5cComposed(t, r.Store, r.SubjectID)
	r.StalePad = w5cStalePadOf(t, r)

	officer := w5cStr(w5cOfficer)
	out := map[string]string{}

	// --- CHECK-IN: the subject is the driver and the sole occupant ---
	// ⟦LOCK — §51/§57, 2026-09-03⟧ §47/§50's ONE re-seed form, through the ONE
	// shared helper (w7_reseed_helper_test.go). Seeded ONCE and carried on the
	// run, so the check-in and the dispatch below name THE SAME truck by
	// construction rather than by relying on the helper's memo.
	// ⚠ §51 BARRED this pending two produced defects; §57 resolves both — BAR 1
	// by normalising the id (w5cNormalise), BAR 2 by re-pinning the three seq
	// literals this event's arrival shifts. Neither is assumed here; both are
	// measured in reports/window-build/S51-BLOCKER-CLEAR.md §9-§11.
	r.VehicleID = w7SeedVehicleID(t, r.Store, w5cSite, w5cVehicle)

	trip, arrival, ciErr := GateCheckIn(r.Store, w5cSite, w5cEpoch, GateCheckInInput{
		// ⟦LOCK — §51/§57, 2026-09-03⟧ ⟦was: "VehicleEntered: w5cVehicle,"⟧
		VehicleID:  r.VehicleID,
		Driver:     PersonSlot{ExistingID: r.SubjectID},
		RecordedBy: officer,
	}, w5cBase, "default")
	r.CheckInTripID = trip.TripID
	r.ArrivalID = arrival.ArrivalID
	out["checkin.err"] = w5cErrText(ciErr)
	w5cFlattenInto(t, out, "checkin.trip", trip)
	w5cFlattenInto(t, out, "checkin.arrival", arrival)

	visits, err := VisitsByTrip(r.Store, r.CheckInTripID)
	if err != nil {
		t.Fatalf("fixture defect: reading the check-in visits: %v", err)
	}
	// ⚠ A FAILED CHECK-IN IS RECORDED, NOT FATAL, AND THAT IS DELIBERATE.
	// The first version of this builder fatalled here with "fixture defect:
	// the check-in produced 0 visits" — and under a REAL (m5c-2) plant that
	// gates the un-oriented person out of check-in, THAT is the message the
	// mutant produced [MEASURED — it did, on the first mutation run]. The
	// plant was caught, but the red said "your fixture is broken" where the
	// truth was "the flag changed the outcome". A refused check-in is part
	// of the outcome and is compared as such; the fixture's own validity is
	// checked AFTER the comparison, where it cannot mask the finding.
	out["checkin.visits.len"] = fmt.Sprint(len(visits))
	if len(visits) == 1 {
		r.VisitID = visits[0].VisitID
		w5cFlattenInto(t, out, "checkin.visit", visits[0])
	}
	w5cCaptureState(t, out, "checkin", r, r.CheckInTripID)

	// --- DISPATCH: a real departure, controlled destination, deadline ---
	dest := w5cStr(w5cPeer)
	deadline := w5cDeadlineMins
	dtrip, dErr := Dispatch(r.Store, w5cSite, w5cEpoch, DispatchInput{
		// ⟦LOCK — §51/§57, 2026-09-03⟧ ⟦was: "VehicleEntered:        w5cVehicle,"⟧
		// The SAME id the check-in used — one truck, two acts.
		VehicleID:             r.VehicleID,
		DriverPersonID:        r.SubjectID,
		OccupantPersonIDs:     []string{r.SubjectID},
		DestinationSite:       dest,
		DeadlineMinutes:       &deadline,
		RecordedBy:            officer,
		ValidateRoster:        true,
		KnownDestinationSites: []string{w5cPeer},
	}, w5cBase, "default")
	r.DispatchTripID = dtrip.TripID
	out["dispatch.err"] = w5cErrText(dErr)
	w5cFlattenInto(t, out, "dispatch.trip", dtrip)
	w5cCaptureState(t, out, "dispatch", r, r.DispatchTripID)

	r.Outcome = w5cNormalise(out, r)
	return r
}

// w5cCaptureState reads back every projection the two acts touch. The point
// of reading them ALL is that "the outcomes are identical" then means the
// system's whole observable answer, not the two structs the calls happened
// to return.
func w5cCaptureState(t *testing.T, out map[string]string, phase string, r *w5cRun, tripID string) {
	t.Helper()
	occ, err := TripOccupantsByTrip(r.Store, tripID)
	if err != nil {
		t.Fatalf("fixture defect: reading trip occupants: %v", err)
	}
	w5cFlattenInto(t, out, phase+".occupants", occ)

	ts, err := TripStateByID(r.Store, tripID)
	if err != nil {
		t.Fatalf("fixture defect: reading trip state: %v", err)
	}
	w5cFlattenInto(t, out, phase+".trip_state", ts)

	as, err := ArrivalStates(r.Store)
	if err != nil {
		t.Fatalf("fixture defect: reading arrival states: %v", err)
	}
	w5cFlattenInto(t, out, phase+".arrival_states", as)

	open, err := OpenVisitsAt(r.Store, w5cSite)
	if err != nil {
		t.Fatalf("fixture defect: reading open visits: %v", err)
	}
	w5cFlattenInto(t, out, phase+".open_visits", open)

	exp, err := OpenExpectations(r.Store, w5cSite)
	if err != nil {
		t.Fatalf("fixture defect: reading open expectations: %v", err)
	}
	w5cFlattenInto(t, out, phase+".open_expectations", exp)

	inc, err := IncompleteCheckIns(r.Store, w5cSite)
	if err != nil {
		t.Fatalf("fixture defect: reading incomplete check-ins: %v", err)
	}
	out[phase+".incomplete_checkins"] = fmt.Sprint(len(inc))
}

func w5cErrText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// w5cFlattenInto marshals v and writes every leaf into out under prefix.
// Reflection-free and COMPLETE BY CONSTRUCTION: a field added to Trip,
// Arrival or Visit appears here without this file being edited, so the
// identity comparison below can never silently stop covering part of the
// result.
func w5cFlattenInto(t *testing.T, out map[string]string, prefix string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("fixture defect: marshalling %s: %v", prefix, err)
	}
	var any0 any
	if err := json.Unmarshal(b, &any0); err != nil {
		t.Fatalf("fixture defect: re-decoding %s: %v", prefix, err)
	}
	w5cWalk(out, prefix, any0)
}

func w5cWalk(out map[string]string, path string, v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			w5cWalk(out, path+"."+k, vv)
		}
	case []any:
		out[path+".len"] = fmt.Sprint(len(x))
		for i, vv := range x {
			w5cWalk(out, fmt.Sprintf("%s[%d]", path, i), vv)
		}
	case nil:
		out[path] = "null"
	default:
		out[path] = fmt.Sprint(x)
	}
}

// w5cNormalise substitutes this run's randomly-minted identities for stable
// placeholders. `newID` is crypto/rand-based (ids.go), so two runs can never
// agree on an id and an unnormalised comparison would be meaningless.
func w5cNormalise(in map[string]string, r *w5cRun) map[string]string {
	subs := []struct{ from, to string }{
		{r.SubjectID, "<PERSON>"},
		{r.CompanyID, "<COMPANY>"},
		// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "		{r.WorkgroupID, \"<WORKGROUP>\"},"⟧ BAR 3.
		{r.TradeID, "<TRADE>"},
		// ⟦LOCK — §57, 2026-09-03⟧ BAR 1 — NEW, and the eighth entry in a table
		// that exists for exactly this. `Trip.VehicleID` is `json:"vehicle_id"`
		// with no omitempty (trip.go:254), so a crypto/rand id reaches the
		// flattened outcome and the two arms — separate stores by construction
		// — can never agree on it.
		// ⚠ NOT A WEAKENING, and the reason is precise: the substitution is
		// CONSISTENT, so every relationship the id encodes survives it. Two
		// arms differing in HOW MANY vehicles they used, or in WHICH act got
		// which vehicle, still differ after normalising — that is how the other
		// seven entries work. What is discarded is an opaque surrogate key. The
		// semantically meaningful value is compared unnormalised and already
		// agrees: vehicle_entered = "TRK-118" in both arms.
		{r.VehicleID, "<VEHICLE>"},
		{r.CheckInTripID, "<TRIP-CHECKIN>"},
		{r.DispatchTripID, "<TRIP-DISPATCH>"},
		{r.ArrivalID, "<ARRIVAL>"},
		{r.VisitID, "<VISIT>"},
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		for _, s := range subs {
			if s.from != "" {
				v = strings.ReplaceAll(v, s.from, s.to)
			}
		}
		out[k] = v
	}
	return out
}

// --- the runtime self-check (standing rule 3 + rule 6) ------------------

// w5cStalePadOf reads the run's discriminator off the LOG: the stale
// `pad_oriented` value carried by the peer's wire amendment, or nil when that
// amendment cleared the key instead. The log is the only place the stale key
// survives at SchemaVersion 11 — no projection column, no composed field.
func w5cStalePadOf(t *testing.T, r *w5cRun) *string {
	t.Helper()
	for _, v := range w5cAuthoredPadValues(t, r.Store, w5cPeer, 7) {
		v := v
		return &v
	}
	return nil
}

// w5cRival is one NAMED alternative rule — a way the system could consult
// the stale key (off the raw payload, since nothing else holds it). Answers reports whether that rival would give the TWO RUNS
// DIFFERENT answers, recomputed FROM READ-BACK FIXTURE DATA, never from the
// fixture constants.
type w5cRival struct {
	Name    string
	Differs func(a, b *w5cRun) bool
}

// w5cRivals are the rules (m5c-2) could take. Each must DISAGREE with the
// certified rule ("nothing consults it", so the outcomes are identical) on
// this fixture — if any rival AGREES, the fixture cannot distinguish the
// certified rule from that rival and the pass is worthless.
var w5cRivals = []w5cRival{
	{
		Name: "refuse the un-oriented person (a dispatch or check-in gate)",
		Differs: func(a, b *w5cRun) bool {
			// Differs iff EXACTLY ONE of the two runs carries the stale key.
			return (a.StalePad == nil) != (b.StalePad == nil)
		},
	},
	{
		Name: "branch on the flag's value anywhere on the path",
		Differs: func(a, b *w5cRun) bool {
			return !w5cSameStr(a.StalePad, b.StalePad)
		},
	},
	{
		Name: "treat only oriented persons as eligible occupants",
		Differs: func(a, b *w5cRun) bool {
			// Differs iff exactly one run carries the stale key — that run's
			// subject is the one such a rule would drop. Whether they
			// ACTUALLY reached the occupant rows is a VACUITY question,
			// checked by w5cVacuityDefects after the comparison, never here:
			// a mutant that gates them out of check-in would otherwise make
			// this rival report a fixture defect and mask its own kill.
			return (a.StalePad == nil) != (b.StalePad == nil)
		},
	},
}

// w5cVacuityDefects returns the reasons a pair of runs that AGREED cannot be
// booked as a pass: two runs that both refused, or both went nowhere, agree
// perfectly and prove nothing.
//
// ⚠ IT IS CHECKED ONLY AFTER THE COMPARISON, AND THE ORDER IS THE POINT. A
// real consultation makes ONE of the two runs fail — so a vacuity check run
// first would fire on the mutant's own effect and report it as a broken
// fixture instead of as the kill it is [MEASURED: the first mutation run did
// exactly that, twice]. If the two outcomes differ, the criterion has
// already failed and the fixture question is moot.
func w5cVacuityDefects(a, b *w5cRun) []string {
	var defects []string
	for _, r := range []*w5cRun{a, b} {
		which := "plain"
		if r.Oriented {
			which = "oriented"
		}
		if r.Outcome["checkin.err"] != "<nil>" {
			defects = append(defects, fmt.Sprintf("the %s run's CHECK-IN failed (%s), so both runs agreeing means only that both went nowhere", which, r.Outcome["checkin.err"]))
		}
		if r.Outcome["dispatch.err"] != "<nil>" {
			defects = append(defects, fmt.Sprintf("the %s run's DISPATCH failed (%s), same reason", which, r.Outcome["dispatch.err"]))
		}
		if r.Outcome["checkin.occupants.len"] != "1" || r.Outcome["dispatch.occupants.len"] != "1" {
			defects = append(defects, fmt.Sprintf("the %s run's subject is not an occupant of both trips (checkin=%s dispatch=%s) — the \"oriented-only occupants\" rival has nothing to drop, so it could not have shown itself", which, r.Outcome["checkin.occupants.len"], r.Outcome["dispatch.occupants.len"]))
		}
		if r.Outcome["checkin.visits.len"] != "1" {
			defects = append(defects, fmt.Sprintf("the %s run produced %s visit(s) at check-in, want exactly 1", which, r.Outcome["checkin.visits.len"]))
		}
	}
	return defects
}

// w5cFixtureDefects returns the reasons this PAIR OF SUBJECTS cannot pose
// W5c-2's case. It is about the two PERSONS — the flag distinguishes them,
// and nothing else does — so it is mutant-independent and is checked FIRST.
// It returns its reasons rather than failing, so the POSITIVE CONTROL below
// can observe the verdict firing (M3).
func w5cFixtureDefects(a, b *w5cRun) []string {
	var defects []string
	for _, rv := range w5cRivals {
		if !rv.Differs(a, b) {
			defects = append(defects, fmt.Sprintf("the rival rule %q gives this fixture THE SAME answer as the certified rule — the fixture cannot tell them apart, so a pass proves nothing", rv.Name))
		}
	}
	// Everything the outcome could legitimately key on, other than the flag,
	// must be EQUAL — otherwise "the outcomes are identical" would be a
	// claim about two different scenarios.
	if a.Subject.Name != b.Subject.Name {
		defects = append(defects, fmt.Sprintf("the two subjects have different names (%q vs %q)", a.Subject.Name, b.Subject.Name))
	}
	// ⟦LOCK — §51, 2026-09-03⟧ ⟦was: "if !w5cSameStr(a.Subject.CompanyID, b.Subject.CompanyID) || !w5cSameStr(a.Subject.WorkgroupID, b.Subject.WorkgroupID) {"⟧
	// §51 transformation (b): W8's Workgroup→Trade rename. `Person` carries
	// `CompanyID` and `TradeID` and no `WorkgroupID` (refdata.go:239/:247).
	//
	// ⚠ THIS IS A COMPARISON KEY — it selects which persons the differential
	// treats as equivalent — so §51 condition 3 required the partition to be
	// MEASURED, not assumed. It was, both ways [reports/window-build/
	// S51-BLOCKER-CLEAR.md §4]: at HEAD both subjects read
	// workgroup="site-b-workgroup-0001"; here both read trade=<absent>. The
	// partition is IDENTICAL — one class, both subjects in it, same verdict —
	// but ⚠ it is identical VACUOUSLY: this fixture seeds `workgroup_id` on a
	// wire `person_created`, and nothing reads that key any more, so no
	// fixture in this file can now make the two subjects differ in trade. The
	// guard is UNEXERCISED, not dead — planting trade-alpha vs trade-beta on
	// the composed persons makes it fire, and equal trades do not (produced,
	// §4). Re-pointing the fixture's own person seeding at `trade_id` would
	// restore the exercise, and that is NOT one of §51's two transformations:
	// it is ROUTED to the plan owner, not done here.
	//
	// The defect message below is left VERBATIM — it is an assertion's text,
	// and §51 permits no assertion change.
	if !w5cSameStr(a.Subject.CompanyID, b.Subject.CompanyID) || !w5cSameStr(a.Subject.TradeID, b.Subject.TradeID) {
		defects = append(defects, "the two subjects differ in company or workgroup")
	}
	for _, f := range []struct {
		what string
		a, b *string
	}{
		{"phone", a.Subject.Phone, b.Subject.Phone},
		{"role", a.Subject.Role, b.Subject.Role},
		{"position", a.Subject.Position, b.Subject.Position},
		{"retired", a.Subject.Retired, b.Subject.Retired},
		{"same_as", a.Subject.SameAs, b.Subject.SameAs},
		{"recorded_by", a.Subject.RecordedBy, b.Subject.RecordedBy},
	} {
		if !w5cSameStr(f.a, f.b) {
			defects = append(defects, fmt.Sprintf("the two subjects differ in %s (%s vs %s) — the fixture varies more than the flag", f.what, w5cPtrStr(f.a), w5cPtrStr(f.b)))
		}
	}
	return defects
}

// TestW5cNonDegeneracySelfCheckPositiveControl — M3's POSITIVE CONTROL, and
// it runs FIRST: the self-check's own verdict is observed firing on a
// DELIBERATELY UNDISTINGUISHED fixture before the real fixture's pass is
// allowed to count for anything.
//
// Without it, "the fixture is non-degenerate" would be true by construction
// — a self-check that never fires is indistinguishable from one that cannot.
//
// EXPECTED RED: a self-check that has stopped discriminating (a rival
// recomputed from the fixture constants instead of from read-back data, a
// rival deleted, a comparison inverted).
func TestW5cNonDegeneracySelfCheckPositiveControl(t *testing.T) {
	// Two runs BOTH carrying the stale key: it no longer distinguishes them,
	// so a consulting path would answer identically and the criterion's
	// pass would be vacuous. The self-check must say so.
	a := w5cBuildSubjectOnly(t, true)
	b := w5cBuildSubjectOnly(t, true)
	defects := w5cFixtureDefects(a, b)
	if len(defects) == 0 {
		t.Fatal("CONTROL FAILED: the non-degeneracy self-check found NO defect in a fixture whose two subjects are BOTH oriented. A self-check that cannot fire is not a check, and W5c-2's pass would then be true by construction (standing rule 5).")
	}
	var sawRefuse, sawBranch bool
	for _, d := range defects {
		if strings.Contains(d, "refuse the un-oriented") {
			sawRefuse = true
		}
		if strings.Contains(d, "branch on the flag") {
			sawBranch = true
		}
	}
	if !sawRefuse || !sawBranch {
		t.Fatalf("CONTROL FAILED: the degenerate fixture was flagged, but not by the two rivals that matter (refuse=%v branch=%v). Defects: %v", sawRefuse, sawBranch, defects)
	}
	t.Logf("fixture-defect verdict OBSERVED on the degenerate fixture (M3's positive control):\n  %s", strings.Join(defects, "\n  "))

	// THE MIRROR: the real pair must NOT be flagged, or the self-check would
	// simply always fire and the control above would prove nothing.
	if d := w5cFixtureDefects(w5cBuildSubjectOnly(t, true), w5cBuildSubjectOnly(t, false)); len(d) != 0 {
		t.Fatalf("CONTROL FAILED (mirror): the self-check flagged the REAL, distinguishing fixture: %v", d)
	}

	// --- the VACUITY check's own positive control: two runs that went
	// nowhere must be reported, or "the outcomes are identical" could be
	// booked over a pair of refusals. ---
	nowhere := []*w5cRun{
		{Oriented: true, Outcome: map[string]string{"checkin.err": "refused", "dispatch.err": "refused", "checkin.occupants.len": "0", "dispatch.occupants.len": "0", "checkin.visits.len": "0"}},
		{Oriented: false, Outcome: map[string]string{"checkin.err": "refused", "dispatch.err": "refused", "checkin.occupants.len": "0", "dispatch.occupants.len": "0", "checkin.visits.len": "0"}},
	}
	if d := w5cVacuityDefects(nowhere[0], nowhere[1]); len(d) == 0 {
		t.Fatal("CONTROL FAILED: the vacuity check found NO defect in a pair of runs that both refused everything — W5c-2 could then be booked green over two identical failures")
	}
	t.Logf("vacuity verdict OBSERVED on a pair of runs that went nowhere: %d defect(s)", len(w5cVacuityDefects(nowhere[0], nowhere[1])))
}

// w5cBuildSubjectOnly builds a subject and reads it back WITHOUT running
// check-in or dispatch — enough for the self-check's positive control, and
// far cheaper than a full passage.
func w5cBuildSubjectOnly(t *testing.T, oriented bool) *w5cRun {
	t.Helper()
	r := &w5cRun{Oriented: oriented, Store: w5cNewStore(t)}
	r.CompanyID = w5cPeer + "-company-0001"
	// ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "	r.WorkgroupID = w5cPeer + \"-workgroup-0001\""⟧
	// ⟦was: "	w5cSeedWorkgroup(t, r.Store, 2, r.WorkgroupID, r.CompanyID, w5cWorkgroupName)"⟧
	// ⟦was: "		\"company_id\": r.CompanyID, \"workgroup_id\": r.WorkgroupID,"⟧
	// BAR 3, the same change as w5cBuildRun's. Kept in step deliberately: the
	// positive control compares subjects built HERE against the criterion's
	// own pair, and a fixture that diverged between the two builders would
	// make the control prove something about a scenario nothing else runs.
	r.TradeID = w5cPeer + "-trade-0001"
	w5cSeedCompany(t, r.Store, 1, r.CompanyID, w5cCompanyName)
	w5cSeedTrade(t, r.Store, 2, r.TradeID, w5cTradeName)
	r.SubjectID = w5cSeedPerson(t, r.Store, 3, w5cSubjectName, map[string]any{
		"company_id": r.CompanyID, "trade_id": r.TradeID,
	})
	w5cOrientOverTheWire(t, r)
	r.Subject = w5cComposed(t, r.Store, r.SubjectID)
	r.StalePad = w5cStalePadOf(t, r)
	r.Outcome = map[string]string{}
	return r
}

// w5cPinnedOutcome is W5c-2's PINNED LITERAL TABLE. The lock requires the
// comparison surface to be "the full check-in result and the full dispatch
// result, field by field against pinned literals — not 'no error' and not a
// smoke assertion". Two runs agreeing on garbage would satisfy an identity
// check alone; these pins are what make the agreement mean the system did
// the right thing.
//
// ⚠ THE TABLE IS A FLOOR, NOT A CEILING. Every pinned path must be present
// and equal; a path NOT in the table is still compared between the two runs
// (that is the criterion) but is not pinned to a literal. That asymmetry is
// deliberate: W5d and W5e are parallel to W5c and add fields to `trip`
// (`cargo_note`) and to the shared machinery, and a table asserted as an
// EXACT key set would red W5c against another sub-chunk's correct build.
// Unpinned paths are logged, so a reader can see what is not pinned.
var w5cPinnedOutcome = map[string]string{
	"checkin.arrival.arrival_id":  "<ARRIVAL>",
	"checkin.arrival.node_epoch":  "1",
	"checkin.arrival.origin_site": "null",
	"checkin.arrival.recorded_by": "Officer Vega",
	// ⚠️ ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "\"checkin.arrival.seq\":                            \"2\","⟧ Re-derived, not incremented: the arrival is the `trip_arrival` at stream position 3.
	"checkin.arrival.seq":      "3",
	"checkin.arrival.site_id":  "site-a",
	"checkin.arrival.trip_ref": "<TRIP-CHECKIN>",
	// ⚠️ ⟦LOCK — W7 wire-literal disposition, plan owner, 2026-09-03; post-lock §44; SEAM-W7-19b carve-out 2 / W7-FOLD-RECORD §4.4⟧ ⟦was: "\"checkin.arrival.vehicle_entered\": \"null\","⟧ **STRUCK, not re-valued.** DR-W7-4 spells absence as OMISSION (`json:"vehicle_entered,omitempty"`, trip.go:130/:221), so this path is no longer EMITTED and the completeness loop below would red it as "ABSENT from the observed outcome". ⚠️ NOT a coverage loss: the absence is now asserted by W7's own criteria (w7_authoring_doors_test.go, w7_correlation_test.go), which is where the OMISSION rule lives. ⚠️ The two PRESENT-value pins at :561 and :599 are UNAFFECTED and deliberately kept — omitempty drops an absent value, not a present one.
	"checkin.arrival_states.len":                   "1",
	"checkin.arrival_states[0].arrival_id":         "<ARRIVAL>",
	"checkin.arrival_states[0].candidates":         "null",
	"checkin.arrival_states[0].disposition":        "expected",
	"checkin.arrival_states[0].trip_id":            "<TRIP-CHECKIN>",
	"checkin.err":                                  "<nil>",
	"checkin.incomplete_checkins":                  "0",
	"checkin.occupants.len":                        "1",
	"checkin.occupants[0].person_id":               "<PERSON>",
	"checkin.occupants[0].trip_id":                 "<TRIP-CHECKIN>",
	"checkin.open_expectations":                    "null",
	"checkin.open_visits.len":                      "1",
	"checkin.open_visits[0].company_snapshot_id":   "<COMPANY>",
	"checkin.open_visits[0].company_snapshot_name": "Kakivik Asset Management",
	"checkin.open_visits[0].is_open":               "1",
	"checkin.open_visits[0].person_id":             "<PERSON>",
	"checkin.open_visits[0].recorded_by":           "Officer Vega",
	"checkin.open_visits[0].site_id":               "site-a",
	// ⚠️ ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "\"checkin.open_visits[0].workgroup_snapshot_id\":   \"<WORKGROUP>\","⟧ ⟦was: "\"checkin.open_visits[0].workgroup_snapshot_name\": \"Crew One\","⟧ **RE-POINTED, and the FIXTURE was made true to meet the pin — the pin was NOT lowered to meet a null.** W8 renamed `Visit.WorkgroupSnapshot*` to `TradeSnapshot*` AND nothing folds `workgroup_created` any more, so these two paths had ALREADY stopped existing — before any re-seed, and no detector reported it. ⛔ §57 BARS the tempting repair: re-pointing alone would have pinned `"Crew One"` down to `null` AND GONE GREEN. `w5cSeedWorkgroup` was therefore replaced by `w5cSeedTrade` through the live fold path (`applyTradeCreated`), and the name below is the SAME LITERAL the locked criterion always asserted. Non-degeneracy PRODUCED in reports/window-build/S51-BLOCKER-CLEAR.md §11.
	"checkin.open_visits[0].trade_snapshot_id":   "<TRADE>",
	"checkin.open_visits[0].trade_snapshot_name": "Crew One",
	"checkin.open_visits[0].trip_id":             "<TRIP-CHECKIN>",
	"checkin.open_visits[0].visit_id":            "<VISIT>",
	"checkin.trip.deadline_minutes":              "null",
	"checkin.trip.destination_site":              "null",
	"checkin.trip.destination_uncontrolled":      "null",
	"checkin.trip.driver_person_id":              "<PERSON>",
	"checkin.trip.node_epoch":                    "1",
	"checkin.trip.origin_site":                   "site-a",
	// ⟦was: "checkin.trip.phase_at_dispatch" and "checkin.trip.phase_override_reason", both "null" — the trip row lost both with weather phases, James 2026-10-04⟧
	"checkin.trip.recorded_by": "Officer Vega",
	// ⚠️ ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "\"checkin.trip.seq\":                               \"1\","⟧ **RE-PINNED — §51's bar on expected values is LIFTED by §57 for these three literals only.** ⚠️ NOT an increment. RE-DERIVED from the produced stream (S51-BLOCKER-CLEAR.md §10): (site-a, epoch 1) now holds `1 vehicle_created / 2 trip_dispatched / 3 trip_arrival / 4 trip_dispatched`, so the check-in trip is the FIRST `trip_dispatched` and sits at 2. ⚠️ The stream also proves exactly ONE event was inserted, not two — which a blind +1 could not have told apart. The old value described a world `W7-4(d)(i)` abolished: a trip now names a vehicle RECORD, so the record must exist first, and a stream with NO `vehicle_created` before a check-in is now the impossible one. ⚠️ Both arms shift IDENTICALLY (produced, §10), so the differential is untouched by this — these are absolute-position assertions, not differential ones.
	"checkin.trip.seq":                    "2",
	"checkin.trip.trip_id":                "<TRIP-CHECKIN>",
	"checkin.trip.vehicle_entered":        "TRK-118",
	"checkin.trip_state.arrival_id":       "<ARRIVAL>",
	"checkin.trip_state.state":            "arrived",
	"checkin.trip_state.trip_id":          "<TRIP-CHECKIN>",
	"checkin.visit.company_snapshot_id":   "<COMPANY>",
	"checkin.visit.company_snapshot_name": "Kakivik Asset Management",
	"checkin.visit.is_open":               "1",
	// SchemaVersion 15 (R-20): the minting envelope's key — the check-in's
	// trip_arrival, which the §57 re-derivation two lines up places at
	// (site-a, epoch 1, seq 3): `1 vehicle_created / 2 trip_dispatched /
	// 3 trip_arrival / 4 trip_dispatched`. Absolute-position pins, like
	// "checkin.trip.seq" — both arms shift identically, so the differential
	// is untouched. is_open above is now DERIVED from these (visit_open.go),
	// and still "1": the open visit of the pair, no later dispatch, no
	// check-out.
	"checkin.visit.node_epoch":           "1",
	"checkin.visit.seq":                  "3",
	"checkin.visit.person_id":            "<PERSON>",
	"checkin.visit.person_snapshot_name": "Aurora, Ada",
	// SchemaVersion 12 (R-16): the import grandfathering marker — see
	// Visit's own field comment (trip.go) and store.go's column comment.
	// A live check-in never sets it; "null" is the correct pin, not a
	// gap — the same convention as "checkin.arrival.origin_site": "null".
	"checkin.visit.predates_orientation_rule": "null",
	"checkin.visit.recorded_by":               "Officer Vega",
	"checkin.visit.site_id":                   "site-a",
	// ⚠️ ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "\"checkin.visit.workgroup_snapshot_id\":            \"<WORKGROUP>\","⟧ ⟦was: "\"checkin.visit.workgroup_snapshot_name\":          \"Crew One\","⟧ The same re-point as the `open_visits` pair above, on the same evidence. Both surfaces read the SAME visit row, which is why they move together — a fixture true for one is true for both.
	"checkin.visit.trade_snapshot_id":        "<TRADE>",
	"checkin.visit.trade_snapshot_name":      "Crew One",
	"checkin.visit.trip_id":                  "<TRIP-CHECKIN>",
	"checkin.visit.visit_id":                 "<VISIT>",
	"dispatch.arrival_states.len":            "1",
	"dispatch.arrival_states[0].arrival_id":  "<ARRIVAL>",
	"dispatch.arrival_states[0].candidates":  "null",
	"dispatch.arrival_states[0].disposition": "expected",
	"dispatch.arrival_states[0].trip_id":     "<TRIP-CHECKIN>",
	"dispatch.err":                           "<nil>",
	"dispatch.incomplete_checkins":           "0",
	"dispatch.occupants.len":                 "1",
	"dispatch.occupants[0].person_id":        "<PERSON>",
	"dispatch.occupants[0].trip_id":          "<TRIP-DISPATCH>",
	"dispatch.open_expectations":             "null",
	"dispatch.open_visits.len":               "0",
	"dispatch.trip.deadline_minutes":         "90",
	"dispatch.trip.destination_site":         "site-b",
	"dispatch.trip.destination_uncontrolled": "null",
	"dispatch.trip.driver_person_id":         "<PERSON>",
	"dispatch.trip.node_epoch":               "1",
	"dispatch.trip.origin_site":              "site-a",
	// ⟦was: "dispatch.trip.phase_at_dispatch" and "dispatch.trip.phase_override_reason", both "null" — the trip row lost both with weather phases, James 2026-10-04⟧
	"dispatch.trip.recorded_by": "Officer Vega",
	// ⚠️ ⟦LOCK — §57, 2026-09-03⟧ ⟦was: "\"dispatch.trip.seq\":                              \"3\","⟧ Re-derived, not incremented: the dispatch is the SECOND `trip_dispatched`, at stream position 4.
	"dispatch.trip.seq":              "4",
	"dispatch.trip.trip_id":          "<TRIP-DISPATCH>",
	"dispatch.trip.vehicle_entered":  "TRK-118",
	"dispatch.trip_state.arrival_id": "null",
	"dispatch.trip_state.state":      "open",
	"dispatch.trip_state.trip_id":    "<TRIP-DISPATCH>",
}

// TestW5cCheckInAndDispatchIgnoreAStalePadOrientedKey — W5c-2, the (b)
// BEHAVIOUR half, on the non-degenerate fixture.
//
// ⟦NO PRE-CHUNK RED⟧ — nothing reads the flag today, so this passes on
// arrival and ITS CLEAN PASS IS NOT EVIDENCE. Its evidence is (m5c-2) — "a
// check-in or dispatch path branching on the flag" — produced against a real
// on-disk tree ON THIS FIXTURE, plus the positive control above.
//
// EXPECTED RED: any build in which the flag reaches a decision on either
// path. The failure names the first differing path with both values, so the
// red says WHICH part of the outcome the flag moved — a refusal, an occupant
// dropped, a state, an expectation.
// ⟦was: TestW5cCheckInAndDispatchIgnorePadOrientation — the discriminator
// moved from the composed field (struck at SchemaVersion 11) to the stale
// key's log bytes; the comparison itself is byte-for-byte the old one⟧
func TestW5cCheckInAndDispatchIgnoreAStalePadOrientedKey(t *testing.T) {
	oriented := w5cBuildRun(t, true)
	plain := w5cBuildRun(t, false)
	w5cRequireStruckShape(t, oriented.Store, "W5c-2 (b)")

	// --- non-degeneracy of the two RUNS, checked at RUNTIME from read-back
	// data. Mutant-independent, so it is safe to check first. -------------
	if defects := w5cFixtureDefects(oriented, plain); len(defects) != 0 {
		t.Fatalf("FIXTURE DEFECT — not a W5c-2 failure. This fixture cannot pose W5c-2's case, so neither its pass nor its failure would mean anything:\n  %s\n  oriented run's stale key: %s\n  plain run's stale key:    %s",
			strings.Join(defects, "\n  "), w5cPtrStr(oriented.StalePad), w5cPtrStr(plain.StalePad))
	}
	// Rule 6, stated positively as well: the two runs must genuinely differ
	// in the stale key's bytes — and NEITHER composes to anything, which is
	// the SchemaVersion-11 half of what this fixture proves.
	if oriented.StalePad == nil || *oriented.StalePad != w5cOriented {
		t.Fatalf("FIXTURE DEFECT: the oriented run's wire amendment does not carry %s=%q on the log (reads %s) — the fixture did not plant the stale key", w5cKeyPadOriented, w5cOriented, w5cPtrStr(oriented.StalePad))
	}
	if plain.StalePad != nil {
		t.Fatalf("FIXTURE DEFECT: the plain run's log carries %s=%s, want none", w5cKeyPadOriented, w5cPtrStr(plain.StalePad))
	}
	for _, r := range []*w5cRun{oriented, plain} {
		if keys := w5cJSONKeys(t, r.Subject); keys[w5cKeyPadOriented] {
			t.Fatalf("W5c-2 / SchemaVersion 11: a composed subject carries %q — the struck fact composed from a stale wire key", w5cKeyPadOriented)
		}
	}

	// --- the two path SETS must match, or the comparison has holes ---
	var onlyOriented, onlyPlain []string
	for k := range oriented.Outcome {
		if _, ok := plain.Outcome[k]; !ok {
			onlyOriented = append(onlyOriented, k)
		}
	}
	for k := range plain.Outcome {
		if _, ok := oriented.Outcome[k]; !ok {
			onlyPlain = append(onlyPlain, k)
		}
	}
	sort.Strings(onlyOriented)
	sort.Strings(onlyPlain)
	if len(onlyOriented)+len(onlyPlain) != 0 {
		t.Errorf("W5c-2 (m5c-2): the two outcomes do not even have the same SHAPE — pad orientation changed which fields exist.\n  only in the ORIENTED run: %v\n  only in the PLAIN run:    %v", onlyOriented, onlyPlain)
	}

	// --- field by field, the criterion itself ---
	var paths []string
	for k := range oriented.Outcome {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	diffs := 0
	for _, p := range paths {
		got, want := oriented.Outcome[p], plain.Outcome[p]
		if got == want {
			continue
		}
		diffs++
		if diffs <= 20 {
			t.Errorf("W5c-2 (m5c-2): pad orientation CHANGED THE OUTCOME at %s\n  oriented subject: %q\n  plain subject:    %q\n  Pad orientation is A RECORD, NOT A GATE ([[D-21]].1, plan :2048) — no check-in, dispatch, arrival, muster or alert path may read it.", p, got, want)
		}
	}
	if diffs > 20 {
		t.Errorf("W5c-2 (m5c-2): … and %d further differing path(s)", diffs-20)
	}

	// --- VACUITY, checked LAST and only when the two runs AGREED. Two runs
	// that both refused agree perfectly and prove nothing; but a real
	// consultation makes exactly ONE of them fail, so checking this first
	// would report the mutant's own effect as a broken fixture. ---
	if diffs == 0 {
		if defects := w5cVacuityDefects(oriented, plain); len(defects) != 0 {
			t.Errorf("FIXTURE DEFECT — not a W5c-2 pass. The two runs agreed, but they agreed on going nowhere:\n  %s", strings.Join(defects, "\n  "))
		}
	}

	// --- against the pinned literals ---
	var unpinned []string
	for _, p := range paths {
		want, ok := w5cPinnedOutcome[p]
		if !ok {
			unpinned = append(unpinned, fmt.Sprintf("%s = %q", p, oriented.Outcome[p]))
			continue
		}
		if got := oriented.Outcome[p]; got != want {
			t.Errorf("W5c-2 PIN: %s = %q, want %q. The two runs agreeing is not enough — they must agree on the RIGHT answer, or a build that broke check-in for everybody would satisfy this criterion.", p, got, want)
		}
	}
	for p, want := range w5cPinnedOutcome {
		if _, ok := oriented.Outcome[p]; !ok {
			t.Errorf("W5c-2 PIN: the pinned path %s (want %q) is ABSENT from the observed outcome — the result shape has changed under this criterion", p, want)
		}
	}
	if len(unpinned) != 0 {
		sort.Strings(unpinned)
		t.Logf("W5c-2: %d observed path(s) are COMPARED between the two runs but not pinned to a literal (see w5cPinnedOutcome's floor-not-ceiling note):\n  %s", len(unpinned), strings.Join(unpinned, "\n  "))
	}
}
