// witness_u4_fold_collision_test.go — witness phase, worktree u4-fold.
//
// Moves z1's §2.3 "behaviour 4" (reports/witness/z1-trust-boundary.md) from a
// CODE READ to a MEASUREMENT. z1 was explicit that it never ran anything:
//
//	"applyVehicleAmended:174 promises 'a non-string value lands as its JSON
//	text' — but applyVehicleCreated decodes the SAME envelope into
//	VehicleCreated{Number *string, Role *string} ... and returns
//	UnmarshalTypeError on "number": 123, so the envelope is behaviour 3
//	[permanent per-stream stall], not behaviour 1. (Code-read + stdlib doc;
//	NOT run.) I did not enumerate the collision matrix across all 17
//	structs; the vehicle pair is one concrete instance."
//
// The mechanism, MEASURED here: every registered fold runs on every applied
// envelope (store/log.go insertAndFold), in projection-NAME order. Nine folds
// json.Unmarshal the payload into a typed struct BEFORE they look at `type`
// (trip, trip_arrival, trip_cancel, vehicle, person, company, trade,
// visit_checkout, arrival_ref_retraction); seven decode into
// map[string]json.RawMessage and coerce tolerantly (the six *_amended folds
// and occurred_at_corrected). encoding/json's documented contract is that a
// wrong-typed field is SKIPPED, decoding completes, and the
// *json.UnmarshalTypeError is returned anyway — so a strict fold errors on a
// payload it would have ignored one line later, and the error is a fold
// error, which store.applyBatch treats as poison: whole-transaction rollback,
// re-apply of the clean prefix, the failing envelope's error returned.
//
// ⟦SchemaVersion 11, G14, 2026-09-08⟧ weather_phase LEFT the strict-ten
// (now nine) when it gained `seen` — the [[D-92]] wire field EVERY other
// carrier already shares, array-typed, not string-typed. This file's own
// MEASURED half assumes every collision is a string/number mismatch (a JSON
// string is always the "control" shape); an array field has no such safe
// control, so applyWeatherPhaseDeclared (weather.go) now peeks `type` on a
// tiny target BEFORE the full strict decode, exactly the guard the six
// *_amended folds already have via their map-decode gate. It is not one of
// the seven tolerant folds either — once `type` matches, the rest of its
// fields decode strictly, same as before — so it now belongs to neither list
// enumerated here and is simply absent from both. The one measured
// consequence: weather_phase also drops out of `recorded_by`'s strict list
// below, since the same type-peek shields it from EVERY foreign field, not
// only `seen` — a real change to a previously-pinned cell, not an oversight.
// ⟦was: "Ten folds … (trip, trip_arrival, trip_cancel, vehicle, person,
// company, trade, visit_checkout, weather_phase, arrival_ref_retraction)"⟧
//
// These tests are GREEN PINS of the tree as it stands. They do not assert
// what the tree SHOULD do — whether a skip / quarantine / dead-letter should
// exist is gate G5, James's scope decision — they assert, in the specific,
// what it DOES do, so that the decision is made on a measurement. If G5 lands
// a repair, the first test below goes red at its "suffix did not land"
// assertion and must be rewritten, not deleted: the collision matrix in the
// second test stays true regardless of what the store does about it.
package domain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// u4Env builds a peer-authored envelope exactly as the sync path delivers
// one (store/c2_test.go's foreignStream shape): schema-stamped, origin ==
// site, payload as given. No AuthoredAt — the store stores it as received
// and nothing here reads it.
func u4Env(site string, epoch, seq int64, payload string) store.Envelope {
	return store.Envelope{
		SiteID: site, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: site,
		Payload: json.RawMessage(payload),
	}
}

// errU4Rollback is the sentinel a probe returns from LocalTx so that a fold
// driven in isolation leaves nothing behind. LocalTx's deferred Rollback
// fires on any non-nil return; the sentinel is how the caller tells "rolled
// back on purpose" from "the probe itself failed".
var errU4Rollback = errors.New("u4: probe rolled back on purpose")

// u4Probe runs fn against a live *sql.Tx on st and ALWAYS rolls it back.
// This is the only way a test in this package can drive one fold by itself:
// store.Store's *sql.DB is unexported by design (C1 review M2) and LocalTx is
// the sanctioned *sql.Tx callback.
func u4Probe(t *testing.T, st *store.Store, fn func(tx *sql.Tx)) {
	t.Helper()
	err := st.LocalTx(func(tx *sql.Tx) error {
		fn(tx)
		return errU4Rollback
	})
	if !errors.Is(err, errU4Rollback) {
		t.Fatalf("probe transaction did not round-trip: %v", err)
	}
}

// u4WantTypeError asserts err wraps a *json.UnmarshalTypeError naming
// exactly (struct, field) — the specific, not a bare err != nil. jsonKind is
// the JSON value kind encoding/json reports ("number", "bool", "array", …).
func u4WantTypeError(t *testing.T, err error, wantStruct, wantField, jsonKind, context string) *json.UnmarshalTypeError {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: returned nil — no type error, the collision did not happen", context)
	}
	var ute *json.UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("%s: error is not a *json.UnmarshalTypeError (a different failure, not the strict-decode collision):\n  %v", context, err)
	}
	if ute.Struct != wantStruct || ute.Field != wantField || ute.Value != jsonKind {
		t.Fatalf("%s: wrong type error — got Struct=%q Field=%q Value=%q, want Struct=%q Field=%q Value=%q\n  %v",
			context, ute.Struct, ute.Field, ute.Value, wantStruct, wantField, jsonKind, err)
	}
	return ute
}

// TestWitnessU4AWireTypeMismatchOnASharedFieldStallsTheStreamAndDropsTheSuffix
// produces z1's exact example end to end and pins what the store does with
// it. The five steps, each a separate assertion so a partial repair is
// visible as a partial change:
//
//	A. the promise — applyVehicleAmended, driven alone, LANDS
//	   vehicle_amended{"number":123} as the text "123" (vehicle_amend.go:174).
//	B. the collision — applyVehicleCreated, driven alone on the SAME
//	   envelope, returns *json.UnmarshalTypeError{Struct:"VehicleCreated",
//	   Field:"number"} — a fold that is NOT the one whose type the payload
//	   names, erroring before it reads `type`.
//	C. the control — the identical three-envelope page with "number":"123"
//	   (a string) applies in full: results [true true true], watermark 3,
//	   the suffix sibling present. The ONLY difference between C and D is
//	   the JSON type of one field.
//	D. the finding — with "number":123 the page returns the vehicle fold's
//	   type error; the PREFIX (seq 1) is committed (z1's "rollback" is
//	   followed by the clean-prefix re-apply, log.go:220-233), the poison
//	   leaves no row and no log entry, and the SUFFIX sibling (seq 3, a
//	   perfectly ordinary company_created) is NOT applied: watermark 1.
//	E. the stall is stable — re-serving the page from wm+1, exactly what the
//	   next pull cycle does, fails identically and moves nothing. That is
//	   the store half of z1's "re-pulled forever"; the engine half
//	   (pullStream never skips, backs off or advances) is a code read in
//	   reports/witness/u4-fold.md, deliberately not run here (repl binds
//	   fixed ports).
func TestWitnessU4AWireTypeMismatchOnASharedFieldStallsTheStreamAndDropsTheSuffix(t *testing.T) {
	const site = "site-p"
	const vehicleID = "site-p-veh-1"
	const suffixCompanyID = "site-p-co-suffix"

	created := u4Env(site, 1, 1, `{"type":"vehicle_created","vehicle_id":"`+vehicleID+`","number":"OLD-1"}`)
	// z1's exact shape: a vehicle_amended whose `number` is a JSON number.
	poison := u4Env(site, 1, 2, `{"type":"vehicle_amended","vehicle_id":"`+vehicleID+`","number":123}`)
	// The sibling: an unrelated, well-formed event later on the same stream.
	suffix := u4Env(site, 1, 3, `{"type":"company_created","company_id":"`+suffixCompanyID+`","name":"Suffix Co"}`)

	// The envelope is structurally valid: it passes the ONLY payload check
	// on the whole ingest path (envelope.go:116 json.Valid) and Validate.
	if !json.Valid(poison.Payload) {
		t.Fatalf("fixture defect: the poison payload is not valid JSON")
	}
	if err := poison.Validate(); err != nil {
		t.Fatalf("Envelope.Validate refused the poison — the store DOES gatekeep this shape and z1 is refuted: %v", err)
	}

	st := newDomainTestStore(t)

	// --- A. the promise, measured in isolation -----------------------------
	u4Probe(t, st, func(tx *sql.Tx) {
		if err := applyVehicleAmended(tx, poison); err != nil {
			t.Fatalf("A: applyVehicleAmended REFUSED its own event — the tolerant fold is not tolerant, z1's premise is wrong: %v", err)
		}
		var landed sql.NullString
		if err := tx.QueryRow(`SELECT number FROM vehicle_amendment WHERE site_id = ? AND node_epoch = ? AND seq = ?`,
			site, 1, 2).Scan(&landed); err != nil {
			t.Fatalf("A: the amendment row was not written: %v", err)
		}
		if !landed.Valid || landed.String != "123" {
			t.Fatalf("A: vehicle_amend.go:174 promises a non-string lands as its JSON text; got number=%v (valid=%v), want \"123\"", landed.String, landed.Valid)
		}
	})

	// --- B. the collision, measured in isolation ---------------------------
	u4Probe(t, st, func(tx *sql.Tx) {
		err := applyVehicleCreated(tx, poison)
		u4WantTypeError(t, err, "VehicleCreated", "number", "number", "B: applyVehicleCreated on a vehicle_amended envelope")
		if !strings.Contains(err.Error(), "domain: vehicle fold:") {
			t.Fatalf("B: the error is not the vehicle fold's own wrapping: %v", err)
		}
	})

	// --- C. the control: same page, the field as a string ------------------
	control := newDomainTestStore(t)
	clean := poison
	clean.Payload = json.RawMessage(`{"type":"vehicle_amended","vehicle_id":"` + vehicleID + `","number":"123"}`)
	results, err := control.ApplyBatch([]store.Envelope{created, clean, suffix})
	if err != nil {
		t.Fatalf("C: the control page failed — the fixture, not the type, is what breaks: %v", err)
	}
	if len(results) != 3 || !results[0] || !results[1] || !results[2] {
		t.Fatalf("C: control results = %v, want [true true true]", results)
	}
	if wm, err := control.Watermark(site, 1); err != nil || wm != 3 {
		t.Fatalf("C: control watermark = %d, %v (want 3)", wm, err)
	}
	if c, err := CompanyByID(control, suffixCompanyID); err != nil || c == nil {
		t.Fatalf("C: the suffix company did not land on the control store (%v, %v) — the sibling is not a clean event and D proves nothing", c, err)
	}

	// --- D. the finding ----------------------------------------------------
	results, err = st.ApplyBatch([]store.Envelope{created, poison, suffix})
	ute := u4WantTypeError(t, err, "VehicleCreated", "number", "number", "D: ApplyBatch([created, poison, suffix])")
	wantFold := `store: fold "vehicle" on (site-p, epoch 1, seq 2)`
	if !strings.Contains(err.Error(), wantFold) {
		t.Fatalf("D: the batch error does not name the vehicle fold on the poison's key.\n  got:  %v\n  want a message containing: %s", err, wantFold)
	}
	// (i) the prefix is COMMITTED — the rollback is followed by a re-apply.
	if len(results) != 1 || !results[0] {
		t.Fatalf("D(i): results = %v, want [true] (the clean prefix re-applied and committed)", results)
	}
	if v, err := VehicleByID(st, vehicleID); err != nil || v == nil {
		t.Fatalf("D(i): the prefix vehicle_created did not land (%v, %v)", v, err)
	}
	// (ii) the watermark rests at the last good seq.
	if wm, err := st.Watermark(site, 1); err != nil || wm != 1 {
		t.Fatalf("D(ii): watermark = %d, %v (want 1: rests below the poison)", wm, err)
	}
	// (iii) the poison left NOTHING: no amendment row, no log row.
	type amendRow struct {
		Seq int64 `json:"seq"`
	}
	if rows, err := dumpSectionWhere[amendRow](st, "vehicle_amendment", store.Eq{Column: "seq", Value: int64(2)}); err != nil || len(rows) != 0 {
		t.Fatalf("D(iii): the poison's vehicle_amendment row was committed: %v, %v", rows, err)
	}
	if evs, err := st.Events(site, 1, 2, 0); err != nil || len(evs) != 0 {
		t.Fatalf("D(iii): event_log holds rows above the watermark: %d rows, %v", len(evs), err)
	}
	// (iv) THE FINDING: the suffix sibling — a clean event that landed on the
	// control store — did not land here. One malformed event discards it.
	if c, err := CompanyByID(st, suffixCompanyID); err != nil {
		t.Fatalf("D(iv): CompanyByID: %v", err)
	} else if c != nil {
		t.Fatalf("D(iv): the suffix company LANDED despite the poison ahead of it — the store now skips or quarantines a poisoned envelope; this witness must be rewritten against the new contract, not deleted: %+v", c)
	}

	// --- E. the stall is stable: the next pull cycle's page ---------------
	// pullStream requests from wm+1 (engine.go:648) — this page.
	results, err = st.ApplyBatch([]store.Envelope{poison, suffix})
	again := u4WantTypeError(t, err, "VehicleCreated", "number", "number", "E: the re-served page")
	if again.Error() != ute.Error() {
		t.Fatalf("E: the retry error differs from the first — a masking failure (the PK-conflict shape TestFoldErrorLeavesNoTraceOfTheFailingEnvelope guards against):\n  first: %v\n  retry: %v", ute, again)
	}
	if len(results) != 0 {
		t.Fatalf("E: the re-served page applied something: results = %v", results)
	}
	if wm, err := st.Watermark(site, 1); err != nil || wm != 1 {
		t.Fatalf("E: watermark moved to %d, %v (want 1: nothing skips)", wm, err)
	}
	if c, err := CompanyByID(st, suffixCompanyID); err != nil || c != nil {
		t.Fatalf("E: the suffix landed on retry (%v, %v)", c, err)
	}
}

// --- the collision matrix ---------------------------------------------------

// u4StrictFold is one of the ten folds that strict-decode before the type
// check: its projection name (the name store.applyBatch's error carries),
// the fold, and the wire struct it decodes into (whose type name
// json.UnmarshalTypeError.Struct reports).
type u4StrictFold struct {
	name string
	fold func(*sql.Tx, store.Envelope) error
	wire any
}

// u4TolerantFold is one of the seven map-decoding folds: its projection
// name, the `type` literal it claims, the fold, its wire struct (the
// vocabulary its authoring edge emits), and a legal-shaped base payload
// (every key a string except where the wire struct says integer) that the
// matrix mutates ONE field of at a time.
type u4TolerantFold struct {
	name string
	typ  string
	fold func(*sql.Tx, store.Envelope) error
	wire any
	base map[string]any
}

// u4StrictFolds: the nine. Projection names from each file's Projection
// literal; if a fold is added or renamed the reflection cross-check below
// goes red by name rather than silently narrowing the matrix.
//
// ⟦SchemaVersion 11, G14⟧ weather_phase (applyWeatherPhaseDeclared) is
// STRUCK from this list — see the file comment. ⟦was: ten, weather_phase
// included⟧
var u4StrictFolds = []u4StrictFold{
	{"arrival_ref_retraction", applyArrivalRefRetracted, ArrivalRefRetraction{}},
	{"company", applyCompanyCreated, CompanyCreated{}},
	{"person", applyPersonCreated, PersonCreated{}},
	{"trade", applyTradeCreated, TradeCreated{}},
	{"trip", applyTripDispatched, TripDispatched{}},
	{"trip_arrival", applyTripArrival, TripArrival{}},
	{"trip_cancel", applyTripCancelled, TripCancelled{}},
	{"vehicle", applyVehicleCreated, VehicleCreated{}},
	{"visit_checkout", applyVisitCheckedOut, VisitCheckedOut{}},
}

// u4TolerantFolds: the seven.
var u4TolerantFolds = []u4TolerantFold{
	{"arrival_amendment", evtArrivalAmended, applyArrivalAmended, ArrivalAmended{},
		map[string]any{"arrival_id": "site-p-arr-1"}},
	{"company_amendment", evtCompanyAmended, applyCompanyAmended, CompanyAmended{},
		map[string]any{"company_id": "site-p-co-1"}},
	{"occurred_at_correction", evtOccurredAtCorrected, applyOccurredAtCorrected, OccurredAtCorrected{},
		map[string]any{"target_site_id": "site-p", "target_node_epoch": 1, "target_seq": 1, "corrected_occurred_at": 1700000000000, "recorded_by": "Officer"}},
	{"person_amendment", evtPersonAmended, applyPersonAmended, PersonAmended{},
		map[string]any{"person_id": "site-p-per-1"}},
	{"trade_amendment", evtTradeAmended, applyTradeAmended, TradeAmended{},
		map[string]any{"trade_id": "site-p-tr-1"}},
	{"trip_amendment", evtTripAmended, applyTripAmended, TripAmended{},
		map[string]any{"trip_id": "site-p-trip-1"}},
	{"vehicle_amendment", evtVehicleAmended, applyVehicleAmended, VehicleAmended{},
		map[string]any{"vehicle_id": "site-p-veh-1"}},
}

// u4Collision is one row of the PINNED matrix: a wire field name that is in
// at least one tolerant event's vocabulary AND on at least one strict
// struct. sources are the tolerant `type` literals that legally carry the
// name; strict are the projection names whose fold errors on it.
type u4Collision struct {
	field   string
	sources []string
	strict  []string
}

// u4PinnedMatrix is the collision matrix as MEASURED 2026-09-05 — EIGHTEEN
// field names (⟦was: NINETEEN — `phase_override_reason` left TripAmended
// and the trip row with weather phases, James 2026-10-04⟧). ⟦was: twenty — `pad_oriented` left BOTH structs (PersonCreated
// strict, PersonAmended tolerant) when SchemaVersion 11 struck it, D-88
// ERRATA; the derived half below would name the drift if it came back⟧ Every strict-side type is string / *string, so the
// triggering mismatch is ANY non-string, non-null JSON value (number, bool,
// array, object); null is safe everywhere (encoding/json ignores null for a
// non-pointer and nils a pointer). The reflection cross-check in the test
// derives this same set from the wire structs' json tags and fails naming
// the drift if a field joins or leaves a struct; the empirical half then
// drives every cell through the real fold.
var u4PinnedMatrix = []u4Collision{
	{"arrival_id", []string{"arrival_amended"}, []string{"arrival_ref_retraction", "trip_arrival"}},
	{"cargo_note", []string{"trip_amended"}, []string{"trip"}},
	{"company_id", []string{"company_amended", "person_amended"}, []string{"company", "person"}},
	{"driver_person_id", []string{"trip_amended"}, []string{"trip"}},
	{"first_name", []string{"person_amended"}, []string{"person"}},
	{"last_name", []string{"person_amended"}, []string{"person"}},
	{"name", []string{"company_amended", "person_amended", "trade_amended"}, []string{"company", "person", "trade"}},
	{"number", []string{"vehicle_amended"}, []string{"vehicle"}},
	{"origin_site", []string{"arrival_amended"}, []string{"trip_arrival"}},
	{"person_id", []string{"person_amended"}, []string{"person"}},
	{"phone", []string{"person_amended"}, []string{"person"}},
	{"position", []string{"person_amended"}, []string{"person"}},
	// ⟦SchemaVersion 11, G14⟧ `weather_phase` STRUCK from this row's strict
	// list: weather_phase left u4StrictFolds entirely (see the file comment),
	// so it can no longer collide on ANY field, recorded_by included. ⟦was:
	// []string{"arrival_ref_retraction", "company", "person", "trade",
	// "trip", "trip_arrival", "trip_cancel", "visit_checkout",
	// "weather_phase"}⟧
	{"recorded_by", []string{"occurred_at_corrected"}, []string{"arrival_ref_retraction", "company", "person", "trade", "trip", "trip_arrival", "trip_cancel", "visit_checkout"}},
	{"role", []string{"person_amended", "vehicle_amended"}, []string{"vehicle"}},
	{"trade_id", []string{"person_amended", "trade_amended"}, []string{"person", "trade"}},
	{"trip_id", []string{"trip_amended"}, []string{"trip", "trip_cancel"}},
	{"vehicle_entered", []string{"arrival_amended", "trip_amended"}, []string{"trip", "trip_arrival"}},
	{"vehicle_id", []string{"vehicle_amended"}, []string{"trip", "trip_arrival", "vehicle"}},
}

// u4JSONFields returns a wire struct's top-level json names (minus `type`)
// mapped to a coarse JSON kind the Go type demands: "string" for string /
// *string, "integer" for int64 / *int64, "array" for slices, "object" for
// struct pointers.
func u4JSONFields(wire any) map[string]string {
	out := map[string]string{}
	rt := reflect.TypeOf(wire)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "type" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.String:
			out[name] = "string"
		case reflect.Int64:
			out[name] = "integer"
		case reflect.Slice:
			out[name] = "array"
		case reflect.Struct:
			out[name] = "object"
		default:
			out[name] = ft.Kind().String()
		}
	}
	return out
}

// TestWitnessU4StrictTolerantFieldCollisionMatrixIsExactlyTheseEighteen pins
// the matrix two ways. ⟦was:
// "TestWitnessU4StrictTolerantFieldCollisionMatrixIsExactlyTheseNineteen" —
// one row (`phase_override_reason`) and one cell left with weather phases,
// James 2026-10-04⟧ ⟦was:
// "TestWitnessU4StrictTolerantFieldCollisionMatrixIsExactlyTheseTwenty" — one
// row (`pad_oriented`) and one cell left with the SchemaVersion 11 strike⟧
//
// DERIVED: the intersection of the seven tolerant wire structs' json names
// with the NINE strict wire structs' json names must equal u4PinnedMatrix
// exactly, and every strict-side kind must be "string". A new field on any
// wire struct that creates or removes a collision fails here by name.
// ⟦was: "the ten strict wire structs'" — weather_phase left u4StrictFolds at
// SchemaVersion 11 (G14); see the file comment⟧
//
// MEASURED: for every (source, field, strict) cell — forty-seven of them
// (⟦was: forty-eight — the phase_override_reason cell left with weather
// phases, James 2026-10-04⟧) —
// a legal-shaped payload of the SOURCE type with that one field set to a
// JSON number is (a) landed by the source's own fold, (b) refused by the
// strict fold with *json.UnmarshalTypeError naming exactly that struct and
// field, and (c) — the control — ignored by the same strict fold when the
// field is a string, proving the JSON type is the only thing that flips it.
// ⟦was: "forty-nine of them" — recorded_by's strict list lost one member
// (weather_phase) with it, at SchemaVersion 11 (G14)⟧
//
// Two extra rows widen "how wide": a non-string `type` and a valid-JSON
// non-object payload each error in ALL ten strict folds and in NONE of the
// seven tolerant ones, and both pass Envelope.Validate.
func TestWitnessU4StrictTolerantFieldCollisionMatrixIsExactlyTheseEighteen(t *testing.T) {
	// --- DERIVED ------------------------------------------------------------
	strictByField := map[string]map[string]string{} // field -> strict name -> kind
	for _, s := range u4StrictFolds {
		for name, kind := range u4JSONFields(s.wire) {
			if strictByField[name] == nil {
				strictByField[name] = map[string]string{}
			}
			strictByField[name][s.name] = kind
		}
	}
	derived := map[string]*u4Collision{}
	for _, tf := range u4TolerantFolds {
		for name := range u4JSONFields(tf.wire) {
			hits, shared := strictByField[name]
			if !shared {
				continue
			}
			row := derived[name]
			if row == nil {
				row = &u4Collision{field: name}
				for sn, kind := range hits {
					if kind != "string" {
						t.Errorf("DERIVED: %s.%s is %s-typed on the strict side — the pinned matrix assumes every collision is string-typed; the triggering mismatch for this cell differs", sn, name, kind)
					}
					row.strict = append(row.strict, sn)
				}
				sort.Strings(row.strict)
				derived[name] = row
			}
			row.sources = append(row.sources, tf.typ)
		}
	}
	var derivedRows []u4Collision
	for _, r := range derived {
		sort.Strings(r.sources)
		derivedRows = append(derivedRows, *r)
	}
	sort.Slice(derivedRows, func(i, j int) bool { return derivedRows[i].field < derivedRows[j].field })
	render := func(rows []u4Collision) string {
		var b strings.Builder
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-22s %-56s -> %s\n", r.field, strings.Join(r.sources, ","), strings.Join(r.strict, ","))
		}
		return b.String()
	}
	if got, want := render(derivedRows), render(u4PinnedMatrix); got != want {
		t.Fatalf("DERIVED: the wire structs' shared field names are not the pinned eighteen.\n--- derived from json tags:\n%s--- pinned (u4PinnedMatrix):\n%s", got, want)
	}
	if len(derivedRows) != 18 { // ⟦was: 19 — weather phases removed, James 2026-10-04⟧
		t.Fatalf("DERIVED: %d colliding field names, the pin says 18", len(derivedRows))
	}

	// --- MEASURED -----------------------------------------------------------
	st := newDomainTestStore(t)
	strictByName := map[string]u4StrictFold{}
	for _, s := range u4StrictFolds {
		strictByName[s.name] = s
	}
	tolerantByType := map[string]u4TolerantFold{}
	for _, tf := range u4TolerantFolds {
		tolerantByType[tf.typ] = tf
	}
	payloadOf := func(tf u4TolerantFold, field string, value any) store.Envelope {
		m := map[string]any{"type": tf.typ}
		for k, v := range tf.base {
			m[k] = v
		}
		m[field] = value
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return u4Env("site-p", 1, 1, string(b))
	}
	cells := 0
	for _, row := range u4PinnedMatrix {
		for _, src := range row.sources {
			tf := tolerantByType[src]
			wrong := payloadOf(tf, row.field, 123)
			right := payloadOf(tf, row.field, "123")
			if err := wrong.Validate(); err != nil {
				t.Fatalf("cell %s.%s: Validate refused a structurally valid envelope: %v", src, row.field, err)
			}
			for _, sn := range row.strict {
				sf := strictByName[sn]
				cells++
				ctx := fmt.Sprintf("cell %s{%q:123} -> fold %q", src, row.field, sn)
				u4Probe(t, st, func(tx *sql.Tx) {
					// (a) the source's own fold lands it.
					if err := tf.fold(tx, wrong); err != nil {
						t.Fatalf("%s: the SOURCE fold %q refused its own wrong-typed field — not tolerant after all: %v", ctx, tf.name, err)
					}
					// (b) the strict fold errors on the same envelope, naming
					// exactly this struct and field.
					u4WantTypeError(t, sf.fold(tx, wrong), reflect.TypeOf(sf.wire).Name(), row.field, "number", ctx)
					// (c) control: as a string, the strict fold ignores the
					// envelope (wrong `type` → return nil, nothing written).
					if err := sf.fold(tx, right); err != nil {
						t.Fatalf("%s: with the field as a STRING the strict fold still errors — the collision is not about JSON type: %v", ctx, err)
					}
				})
			}
		}
	}
	if cells != 47 { // ⟦was: 48 — phase_override_reason left, weather phases removed 2026-10-04; ⟦was: 49 — recorded_by lost weather_phase, SchemaVersion 11 (G14)⟧⟧
		t.Fatalf("measured %d cells, the pin says 47", cells)
	}

	// --- the two widening rows ---------------------------------------------
	for _, extra := range []struct {
		label, payload, field, jsonKind string
	}{
		// A non-string `type`: every strict struct's first field.
		{"non-string type", `{"type":7,"vehicle_id":"site-p-veh-1"}`, "type", "number"},
		// A valid-JSON non-object: decodes into no struct at all. Struct and
		// Field are empty on this shape (the error is about the root value).
		{"non-object payload", `[]`, "", "array"},
	} {
		env := u4Env("site-p", 1, 1, extra.payload)
		if err := env.Validate(); err != nil {
			t.Fatalf("%s: Validate refused it — the store gatekeeps this shape: %v", extra.label, err)
		}
		u4Probe(t, st, func(tx *sql.Tx) {
			for _, sf := range u4StrictFolds {
				wantStruct := reflect.TypeOf(sf.wire).Name()
				if extra.field == "" {
					wantStruct = ""
				}
				u4WantTypeError(t, sf.fold(tx, env), wantStruct, extra.field, extra.jsonKind, extra.label+" -> fold "+sf.name)
			}
			for _, tf := range u4TolerantFolds {
				if err := tf.fold(tx, env); err != nil {
					t.Fatalf("%s: tolerant fold %q errored: %v", extra.label, tf.name, err)
				}
			}
		})
	}
}
