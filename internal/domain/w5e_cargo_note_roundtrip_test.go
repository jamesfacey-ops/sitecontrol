// w5e_cargo_note_roundtrip_test.go — W5e's behavioural half, item 3.
// design/WINDOW-BUILD-PLAN-v1.md "#### W5e — item 3: cargo note", LOCKED
// 2026-08-31.
//
// Rows discharged here:
//
//	W5e-1  (round-trip half)   — the payload field is stored BYTE-FAITHFULLY:
//	                             no trim, no normalise, no case-fold, no
//	                             truncation.
//	W5e-1b (wire-literal half) — the JSON tag is EXACTLY `cargo_note`,
//	                             asserted against the MARSHALLED PAYLOAD
//	                             BYTES (and against the authored envelope's
//	                             bytes, which are what the row hash covers),
//	                             never against a Go identifier.
//	W5e-2  (behaviour half)    — trips with cargo "", absent, "empty",
//	                             "none" and a 2 KB string are treated
//	                             IDENTICALLY by every path.
//
// EXPECTED RED — this file names `TripDispatched.CargoNote`,
// `Trip.CargoNote` and `DispatchInput.CargoNote`, NONE of which exists at
// 88dd6a6 (`grep -arn CargoNote --include=*.go .` -> 0 [MEASURED]). The whole
// `internal/domain` test binary therefore FAILS TO BUILD until the builder
// lands the field. That compile failure IS this file's pre-chunk red and it
// is deliberate (plan §5 step 2: "capture the go vet output as its red").
// After the field lands:
//   - (m5e-1) trimmed / case-folded / truncated / NFC-normalised:
//     TestW5eCargoNoteStoredByteFaithfully, which compares the read-back
//     against the input BYTE FOR BYTE over a vector chosen to make each
//     mutation visible on its own.
//   - (m5e-1b) spelled `cargo` / `cargoNote` / `note`:
//     TestW5eCargoNoteWireLiteralIsPinned, on the marshalled bytes.
//   - (m5e-2) any taxonomy or flag derived from content:
//     TestW5eCargoContentChangesNothingDownstream, whose five runs must
//     produce a byte-identical canonical projection dump and byte-identical
//     read-path results.
//
// SEAM W5e-S-EDGE — bound here, ONCE. No document names the AUTHORING EDGE's
// field. The locked criteria name the payload (`TripDispatched.CargoNote`),
// the projection row (`Trip.CargoNote`, named inside W5e-2's own filter) and
// the column (`cargo_note`), and are silent on how a caller supplies it.
// This suite binds `DispatchInput.CargoNote *string` — every other optional
// trip fact rides `DispatchInput` (DestinationSite, DeadlineMinutes,
// RecordedBy), and `Dispatch`'s parameter list is settled. A builder that
// wants a different edge disputes this at the gate; it is not an edit to
// this file.
package domain

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"sitecontrol/internal/store"
)

// --- fixture ----------------------------------------------------------------

// w5eWorld is a self-contained two-site fixture. It deliberately does NOT
// reuse c6World: W5e runs parallel to four other sub-chunks and a shared
// helper is a shared edit site (W5-X3's own lesson).
type w5eWorld struct {
	t                 *testing.T
	a, b              *store.Store
	company           Company
	trade             Trade
	driver, passenger Person
	checkInTrip       Trip
	checkInArrival    Arrival
}

func w5eNewWorld(t *testing.T) *w5eWorld {
	t.Helper()
	w := &w5eWorld{t: t, a: newDomainTestStore(t), b: newDomainTestStore(t)}
	var err error
	if w.company, err = CreateCompany(w.a, "site-a", 1, "Kakivik Asset Management", 0, "default"); err != nil {
		t.Fatalf("fixture: company: %v", err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "if w.trade, err = CreateTrade(w.a, "site-a", 1, w.company.CompanyID, "Inspection", 0, "default"); err != nil {"⟧
	if w.trade, err = CreateTrade(w.a, "site-a", 1, "Inspection", 0, "default"); err != nil {
		t.Fatalf("fixture: workgroup: %v", err)
	}
	if w.driver, err = CreatePerson(w.a, "site-a", 1, "Dana Driver", &w.company.CompanyID, &w.trade.TradeID, 0, "default"); err != nil {
		t.Fatalf("fixture: driver: %v", err)
	}
	if w.passenger, err = CreatePerson(w.a, "site-a", 1, "Pat Passenger", &w.company.CompanyID, &w.trade.TradeID, 0, "default"); err != nil {
		t.Fatalf("fixture: passenger: %v", err)
	}
	// Put both people on site-a's on-site list through the §10 degenerate
	// local trip, exactly as the gate check-in does.
	if w.checkInTrip, err = DispatchLocalCheckIn(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.driver.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.a, "site-a", "gate walk-in"), DriverPersonID: w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("fixture: local check-in dispatch: %v", err)
	}
	if w.checkInArrival, err = RecordExpectedArrival(w.a, "site-a", 1, w.checkInTrip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("fixture: local check-in arrival: %v", err)
	}
	return w
}

// w5eDepart authors ONE ordinary departure carrying the given cargo note.
func (w *w5eWorld) depart(cargo *string) Trip {
	w.t.Helper()
	dest := "site-b"
	deadline := int64(60)
	recordedBy := "Officer Osgood"
	trip, err := Dispatch(w.a, "site-a", 1, DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered:    "TRUCK 12","⟧
		VehicleID:         w7SeedVehicleID(w.t, w.a, "site-a", "TRUCK 12"),
		DriverPersonID:    w.driver.PersonID,
		OccupantPersonIDs: []string{w.passenger.PersonID},
		DestinationSite:   &dest,
		DeadlineMinutes:   &deadline,
		RecordedBy:        &recordedBy,
		CargoNote:         cargo, // SEAM W5e-S-EDGE
	}, 0, "default")
	if err != nil {
		w.t.Fatalf("W5e-3: a departure carrying cargo note %v was REFUSED: %v — W5e adds a recorded field, not a rule", w5eShow(cargo), err)
	}
	return trip
}

func (w *w5eWorld) events(st *store.Store, siteID string) []store.Envelope {
	w.t.Helper()
	evs, err := st.Events(siteID, 1, 1, 0)
	if err != nil {
		w.t.Fatalf("events(%s): %v", siteID, err)
	}
	return evs
}

func (w *w5eWorld) syncBFromA() {
	w.t.Helper()
	for _, ev := range w.events(w.a, "site-a") {
		if _, err := w.b.Apply(ev); err != nil {
			w.t.Fatalf("sync B from A: %v", err)
		}
	}
}

func w5ePtr(s string) *string { return &s }

func w5eShow(p *string) string {
	if p == nil {
		return "<absent>"
	}
	if len(*p) > 40 {
		return "\"" + (*p)[:40] + "…\" (" + w5eItoa(len(*p)) + " bytes)"
	}
	return "\"" + *p + "\""
}

func w5eItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- W5e-1: byte fidelity ---------------------------------------------------

// w5eByteVectors are chosen so that EACH forbidden mutation is visible on its
// own, and no two are covered by the same vector (rule 5: a test must be
// capable of failing, one reason at a time).
var w5eByteVectors = []struct {
	Name  string
	Value string
	Kills string
}{
	{"leading and trailing whitespace", "  3 pallets drill pipe \t\n", "(m5e-1) trim"},
	{"mixed case", "Two Drums DIESEL, one Pallet", "(m5e-1) case-fold"},
	{"internal double spacing", "rig  mats   x4", "(m5e-1) whitespace collapse — the NormalizeVehicleID shape, applied to the wrong field"},
	{"NFD combining sequence", "café supplies", "(m5e-1) Unicode NFC normalisation — trip.go's own F12 fix applied where it must NOT be"},
	{"2 KB", strings.Repeat("drill pipe, ", 170) + "end", "(m5e-1) truncation, and (m5e-3) a length cap"},
	{"SQL metacharacters", "1 x pipe'; DROP TABLE trip; --", "escaping/rewriting on the way in"},
	{"empty string", "", "(m5e-1) empty coerced to NULL — see the note on TestW5eCargoNoteStoredByteFaithfully"},
}

// TestW5eCargoNoteStoredByteFaithfully — W5e-1's round-trip half.
//
// ⚠️ THE EMPTY-STRING VECTOR IS AN INTERPRETATION, and it is flagged rather
// than smuggled. W5e-1 says the field is stored "byte-faithfully — no trim,
// no normalise". Coercing `""` to SQL NULL is a normalise, so this suite
// asserts that an empty note stores as a non-NULL empty string, distinct
// from absent. That is NOT in tension with W5e-2's "`\"\"` and absent are
// treated identically by every path": W5e-2 governs what code DOES with the
// note, W5e-1 governs what STORAGE does with the bytes. If the gate reads
// W5e-1 the other way, this one sub-assertion is the thing to strike —
// reported in reports/window-build/W5e-tests.md §7.3.
func TestW5eCargoNoteStoredByteFaithfully(t *testing.T) {
	for _, vec := range w5eByteVectors {
		vec := vec
		t.Run(vec.Name, func(t *testing.T) {
			w := w5eNewWorld(t)
			trip := w.depart(w5ePtr(vec.Value))

			// Rule 6 — assert the ARRANGEMENT: the vector must actually be
			// the thing it claims to be, or the assertion below defends
			// nothing. A vector that has already been normalised by the test
			// itself cannot detect normalisation by the code.
			if vec.Name == "NFD combining sequence" && !strings.ContainsRune(vec.Value, '́') {
				t.Fatalf("fixture defect: the NFD vector carries no combining acute — the NFC-normalisation mutant could not be detected")
			}
			if !utf8.ValidString(vec.Value) {
				t.Fatalf("fixture defect: vector %q is not valid UTF-8; the JSON wire cannot carry it faithfully and the assertion would fail for the wrong reason", vec.Name)
			}

			// Channel 1 — the RAW COLUMN, read straight out of SQLite with
			// no Go struct and no JSON in between.
			got, isNull := w5eReadCargoColumn(t, w, trip.TripID)
			if isNull {
				t.Fatalf("W5e-1 (m5e-1): trip.cargo_note is NULL after authoring %s — an authored note must not be dropped", w5eShow(&vec.Value))
			}
			if got != vec.Value {
				t.Fatalf("W5e-1 (m5e-1): trip.cargo_note round-tripped as %q, want %q byte for byte. Kills: %s. The note is the officer's own words about what is on the truck; every rewrite is a different record of the load",
					got, vec.Value, vec.Kills)
			}

			// Channel 2 — the projection ROW STRUCT (Trip.CargoNote), the
			// spelling W5e-2's own filter names.
			rows, err := Trips(w.a)
			if err != nil {
				t.Fatalf("Trips: %v", err)
			}
			var found *Trip
			for i := range rows {
				if rows[i].TripID == trip.TripID {
					found = &rows[i]
				}
			}
			if found == nil {
				t.Fatalf("W5e-1: the departure %s is not in the trip projection at all", trip.TripID)
			}
			if found.CargoNote == nil {
				t.Fatalf("W5e-1 (m5e-1): Trip.CargoNote is nil after authoring a note")
			}
			if *found.CargoNote != vec.Value {
				t.Fatalf("W5e-1 (m5e-1): Trip.CargoNote = %q, want %q byte for byte", *found.CargoNote, vec.Value)
			}

			// Channel 3 — the value Dispatch itself handed back, so a
			// normalisation applied on the way OUT of the edge is caught too.
			if trip.CargoNote == nil || *trip.CargoNote != vec.Value {
				t.Fatalf("W5e-1 (m5e-1): Dispatch returned CargoNote %v, want %q — a rewrite at the edge is still a rewrite", w5eShow(trip.CargoNote), vec.Value)
			}
		})
	}
}

// TestW5eCargoNoteAbsentIsNullAndDistinctFromEmpty — W5e-1's nullability half
// at the value level, the mirror of the column-level assertion in
// internal/store/w5e_cargo_note_structural_test.go.
func TestW5eCargoNoteAbsentIsNullAndDistinctFromEmpty(t *testing.T) {
	w := w5eNewWorld(t)
	absent := w.depart(nil)
	empty := w.depart(w5ePtr(""))

	// Rule 6 — the two trips must be genuinely different rows, or "distinct"
	// is measured against one row twice.
	if absent.TripID == empty.TripID {
		t.Fatalf("fixture defect: both departures minted the same trip id %s", absent.TripID)
	}

	gotAbsent, absentIsNull := w5eReadCargoColumn(t, w, absent.TripID)
	if !absentIsNull {
		t.Fatalf("W5e-1 (m5e-1): an ABSENT cargo note stored as %q rather than SQL NULL — the column is nullable and absent means absent (\"absent = not hauling\")", gotAbsent)
	}
	gotEmpty, emptyIsNull := w5eReadCargoColumn(t, w, empty.TripID)
	if emptyIsNull {
		t.Fatalf("W5e-1 (m5e-1): an EMPTY cargo note was coerced to SQL NULL — that is a normalisation, and W5e-1 forbids normalising the stored bytes. (If the gate reads W5e-1 the other way, strike this assertion: see reports/window-build/W5e-tests.md §7.3)")
	}
	if gotEmpty != "" {
		t.Fatalf("W5e-1: an empty cargo note stored as %q", gotEmpty)
	}
}

// w5eReadCargoColumn reads trip.cargo_note straight out of SQLite. Reading
// the raw column rather than a decoded struct is the point: it is the only
// channel with no Go type and no JSON codec able to hide a rewrite.
func w5eReadCargoColumn(t *testing.T, w *w5eWorld, tripID string) (string, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", w.a.Path)
	if err != nil {
		t.Fatalf("open raw store: %v", err)
	}
	defer db.Close()
	var v sql.NullString
	if err := db.QueryRow(`SELECT cargo_note FROM trip WHERE trip_id = ?`, tripID).Scan(&v); err != nil {
		t.Fatalf("W5e-1: reading trip.cargo_note for %s: %v", tripID, err)
	}
	return v.String, !v.Valid
}

// --- W5e-1b: the wire literal ----------------------------------------------

// w5eRivalWireKeys are (m5e-1b)'s named misspellings plus the obvious near
// misses. Any of them appearing as a payload key is the mutant.
var w5eRivalWireKeys = []string{"cargo", "cargoNote", "cargonote", "CargoNote", "note", "notes", "manifest", "cargo_notes"}

// TestW5eCargoNoteWireLiteralIsPinned — W5e-1b. Asserted as a LITERAL against
// the marshalled payload bytes, never against a Go identifier.
//
// Why this criterion exists, in one line from the lock: the spelling is
// frozen into hashed bytes and W9a's LOCKED criteria already write
// `cargo_note` (plan:2254; design/trip-fact-correction-v1.md:211 labels it
// "item 3's field"). A `cargo` spelling here costs a both-posts-down respell
// later. W5e is the chunk that freezes the bytes first.
func TestW5eCargoNoteWireLiteralIsPinned(t *testing.T) {
	const note = "3 pallets drill pipe"

	// Channel 1 — the payload struct marshalled directly.
	raw, err := json.Marshal(TripDispatched{
		Type: "trip_dispatched", TripID: "site-a-trip-0000000000000001",
		// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "VehicleEntered: \"TRUCK 12\","⟧
		VehicleEntered: w5ePtr("TRUCK 12"), DriverPersonID: "site-a-person-0000000000000001",
		OccupantPersonIDs: []string{"site-a-person-0000000000000001"},
		CargoNote:         w5ePtr(note),
	})
	if err != nil {
		t.Fatalf("marshal TripDispatched: %v", err)
	}
	w5eAssertWireKey(t, "TripDispatched marshalled directly", raw, note)

	// Channel 2 — the AUTHORED ENVELOPE's payload bytes. These are the bytes
	// Envelope.Hash() covers and the bytes that replicate, so this is the
	// channel the freeze is actually about.
	w := w5eNewWorld(t)
	trip := w.depart(w5ePtr(note))
	var payload []byte
	for _, ev := range w.events(w.a, "site-a") {
		var probe struct {
			Type   string `json:"type"`
			TripID string `json:"trip_id"`
		}
		if err := json.Unmarshal(ev.Payload, &probe); err != nil {
			continue
		}
		if probe.Type == "trip_dispatched" && probe.TripID == trip.TripID {
			payload = ev.Payload
		}
	}
	if payload == nil {
		t.Fatalf("W5e-1b fixture defect: no trip_dispatched envelope for %s was found in site-a's stream", trip.TripID)
	}
	w5eAssertWireKey(t, "authored envelope payload", payload, note)

	// The ABSENT case: no cargo-shaped key may carry a value. Absent or
	// explicit null are both lawful (`omitempty` is not pinned by the lock);
	// a rival SPELLING is not.
	absentTrip := w.depart(nil)
	for _, ev := range w.events(w.a, "site-a") {
		var probe struct {
			Type   string `json:"type"`
			TripID string `json:"trip_id"`
		}
		if err := json.Unmarshal(ev.Payload, &probe); err != nil || probe.TripID != absentTrip.TripID {
			continue
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(ev.Payload, &keys); err != nil {
			t.Fatalf("unmarshal absent-cargo payload: %v", err)
		}
		for _, rival := range w5eRivalWireKeys {
			if _, ok := keys[rival]; ok {
				t.Fatalf("W5e-1b (m5e-1b): the absent-cargo payload carries the rival key %q. The pinned literal is %q and nothing else", rival, "cargo_note")
			}
		}
		if v, ok := keys["cargo_note"]; ok && string(v) != "null" {
			t.Fatalf("W5e-1b: an ABSENT cargo note serialised as cargo_note=%s, want the key omitted or null", v)
		}
	}
}

func w5eAssertWireKey(t *testing.T, channel string, payload []byte, want string) {
	t.Helper()
	if !strings.Contains(string(payload), `"cargo_note":`) {
		t.Fatalf("W5e-1b (m5e-1b) [%s]: the marshalled payload does not carry the literal `\"cargo_note\":`. The wire field name is PINNED — W9a's LOCKED criteria already restrict `Clear` to `cargo_note`/`phase_override_reason` (plan:2254), and this chunk freezes the bytes first. Payload: %s", channel, string(payload))
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(payload, &keys); err != nil {
		t.Fatalf("[%s] unmarshal payload: %v", channel, err)
	}
	rawVal, ok := keys["cargo_note"]
	if !ok {
		t.Fatalf("W5e-1b (m5e-1b) [%s]: no `cargo_note` key in the payload. Keys present: %v", channel, w5eSortedKeys(keys))
	}
	var got string
	if err := json.Unmarshal(rawVal, &got); err != nil {
		t.Fatalf("W5e-1b [%s]: cargo_note is not a JSON string: %s", channel, rawVal)
	}
	if got != want {
		t.Fatalf("W5e-1b/W5e-1 [%s]: cargo_note carried %q on the wire, want %q", channel, got, want)
	}
	for _, rival := range w5eRivalWireKeys {
		if _, ok := keys[rival]; ok {
			t.Fatalf("W5e-1b (m5e-1b) [%s]: the payload carries the rival key %q beside %q. A second spelling of a field frozen into hashed bytes is a both-posts-down respell later", channel, rival, "cargo_note")
		}
	}
}

func w5eSortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- W5e-2: the behaviour half ---------------------------------------------

// w5eBehaviourValues are the five the lock names, verbatim: "cargo `\"\"`,
// absent, `\"empty\"`, `\"none\"` and a 2 KB string".
var w5eBehaviourValues = []struct {
	Name  string
	Value *string
}{
	{"absent", nil},
	{"empty string", w5ePtr("")},
	{"the word empty", w5ePtr("empty")},
	{"the word none", w5ePtr("none")},
	{"2 KB", w5ePtr(strings.Repeat("drill pipe, ", 170) + "end")},
}

// TestW5eCargoContentChangesNothingDownstream — W5e-2's behaviour half.
// "Absent = not hauling. No flag, no taxonomy, no code branches on cargo
// content." Five otherwise-identical departures, five arrivals, and the whole
// folded state plus every read path must come out byte-identical once the
// note itself and the minted identities are canonicalised away.
//
// Read beside the scan half in internal/store/w5e_content_scan_test.go: the
// scan catches the branch structurally, this catches it behaviourally, and
// neither alone is the criterion.
func TestW5eCargoContentChangesNothingDownstream(t *testing.T) {
	type run struct {
		name      string
		canonical string
		stored    string
		isNull    bool
	}
	var runs []run

	for _, bv := range w5eBehaviourValues {
		w := w5eNewWorld(t)
		trip := w.depart(bv.Value)
		w.syncBFromA()
		arrival, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default")
		if err != nil {
			t.Fatalf("W5e-2 (m5e-2): the run with cargo %s could not complete its EXPECTED ARRIVAL: %v.\nThe four other runs differ from this one in the cargo note and nothing else, so a cargo-driven behaviour difference is the first thing to look at — a fold that branches on the note can leave a trip with no occupant rows, which is what this refusal reports. (If every run fails here identically it is a fixture defect, not m5e-2.)", w5eShow(bv.Value), err)
		}
		// An UNSOLICITED arrival too — the §9.2 correlation path is the one
		// D-21.3 is actually about, and its key is the vehicle id. Cargo must
		// not reach it.
		// ⚠ ⟦ROUTED — §50 arrival re-seed, §47 condition 3 (P4), 2026-09-03⟧ NOT RE-SEEDED. This
		// run's dump is canonicalised and compared ACROSS the five cargo values, and
		// `w5eRequireNoMintedIDs` fails the run outright on ANY leftover `site-x-y-<16 hex>` — a
		// FIXTURE-DEFECT guard, not an assertion about cargo. A seeded arrival puts a second minted
		// vehicle id into the dump with no entry in the `ids` map. That is an in-scope check that
		// depends on the dump's exact content, so P4 fails.
		// ⚠⚠ AND A FINDING FOR THE PLAN OWNER, RECORDED HERE BECAUSE IT IS NOT MINE TO FIX: the
		// §47 TRIP half already trips that same guard. `w.depart` now seeds a vehicle record, `trip`
		// carries a `vehicle_id` column (trip.go), and that id is in the dump and in no `ids` entry.
		// This file cannot currently be run — it also carries another pass's `CreateTrade` arity
		// error — so the defect is MASKED. Adding the vehicle ids to the `ids` map is a fixture
		// change beyond either re-seed's stated form. See reports/window-build/ARRIVAL-RESEED.md.
		unsol, err := RecordUnsolicitedArrival(w.b, "site-b", 1, "TRUCK 12", "site-a",
			[]string{w.driver.PersonID}, nil, 0, "default")
		if err != nil {
			t.Fatalf("W5e-2 (m5e-2) / [[D-21]].3: the run with cargo %s could not record an UNSOLICITED ARRIVAL: %v.\nThis is the §9.2 correlation path D-21.3 is actually about; its key is the vehicle id and the cargo note must not reach it.", w5eShow(bv.Value), err)
		}

		// Replay BOTH streams into a blank store: the fold is what the
		// criterion is about, not the authoring stores' incidental state.
		merged := newDomainTestStore(t)
		for _, ev := range append(append([]store.Envelope{}, w.events(w.a, "site-a")...), w.events(w.b, "site-b")...) {
			if _, err := merged.Apply(ev); err != nil {
				t.Fatalf("[%s] merged apply %v: %v", bv.Name, ev.Key(), err)
			}
		}

		ids := map[string]string{
			w.company.CompanyID:        "<company>",
			w.trade.TradeID:            "<workgroup>",
			w.driver.PersonID:          "<driver>",
			w.passenger.PersonID:       "<passenger>",
			w.checkInTrip.TripID:       "<checkin-trip>",
			w.checkInArrival.ArrivalID: "<checkin-arrival>",
			trip.TripID:                "<departure-trip>",
			arrival.ArrivalID:          "<expected-arrival>",
			unsol.ArrivalID:            "<unsolicited-arrival>",
		}

		dump, err := merged.DumpProjections()
		if err != nil {
			t.Fatalf("[%s] dump: %v", bv.Name, err)
		}
		canonical := w5eCanonicaliseDump(t, bv.Name, dump, ids)

		// Every READ PATH that could plausibly consult a trip fact, appended
		// to the same canonical string. A dump is at-rest state; these are
		// the derivations.
		for _, probe := range []struct {
			name string
			get  func() (any, error)
		}{
			{"OpenExpectations(site-b)", func() (any, error) { return OpenExpectations(merged, "site-b") }},
			{"OpenExpectations(site-a)", func() (any, error) { return OpenExpectations(merged, "site-a") }},
			{"IncompleteCheckIns(site-a)", func() (any, error) { return IncompleteCheckIns(merged, "site-a") }},
			{"InTransitFrom(site-a)", func() (any, error) { return InTransitFrom(merged, "site-a") }},
			{"TripStates", func() (any, error) { return TripStates(merged) }},
			{"TripOccupants", func() (any, error) { return TripOccupants(merged) }},
		} {
			v, err := probe.get()
			if err != nil {
				t.Fatalf("[%s] %s: %v", bv.Name, probe.name, err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("[%s] marshal %s: %v", bv.Name, probe.name, err)
			}
			canonical += "\n### " + probe.name + "\n" + w5eCanonicaliseJSON(t, bv.Name, b, ids)
		}

		stored, isNull := w5eReadCargoColumn(t, w, trip.TripID)
		runs = append(runs, run{name: bv.Name, canonical: canonical, stored: stored, isNull: isNull})
	}

	// Rule 6 — THE NON-DEGENERACY GUARD, and without it this whole test is
	// vacuous: the five runs must genuinely have stored five different notes.
	// If storage flattened them, "identical downstream" would be true for the
	// wrong reason and the criterion would certify nothing.
	seen := map[string]string{}
	for _, r := range runs {
		key := "NULL"
		if !r.isNull {
			key = "S:" + r.stored
		}
		if prev, dup := seen[key]; dup {
			t.Fatalf("W5e-2 FIXTURE DEFECT (non-degeneracy): runs %q and %q stored the SAME cargo note %q. The five values must differ at rest, or \"treated identically by every path\" is measured over one value five times and proves nothing", prev, r.name, key)
		}
		seen[key] = r.name
	}
	if len(seen) != len(w5eBehaviourValues) {
		t.Fatalf("W5e-2 FIXTURE DEFECT: %d distinct stored notes across %d runs", len(seen), len(w5eBehaviourValues))
	}

	// The assertion.
	for i := 1; i < len(runs); i++ {
		if runs[i].canonical != runs[0].canonical {
			t.Fatalf("W5e-2 (m5e-2): the run with cargo %q produced DIFFERENT folded state or read-path output from the run with cargo %q. Absent = not hauling: no flag, no taxonomy, no code branches on cargo content — the note is a record for a human to read, and any behaviour derived from it is a manifest system D-21.3 explicitly declined (\"we won't have manifests anyway so moot\").\n%s",
				runs[i].name, runs[0].name, w5eFirstDifference(runs[0].canonical, runs[i].canonical))
		}
	}
}

// --- canonicalisation -------------------------------------------------------

var w5eMintedID = regexp.MustCompile(`site-[a-z]+-[a-z_]+-[0-9a-f]{16}`)

// w5eCanonicaliseDump renders a projection dump comparable across runs:
// minted identities are replaced by role tokens, the cargo note itself is
// removed (it is the independent variable), and rows are re-sorted by their
// canonical form so a random-id sort order cannot masquerade as a behaviour
// difference.
func w5eCanonicaliseDump(t *testing.T, runName string, dump []byte, ids map[string]string) string {
	t.Helper()
	var tables map[string][]map[string]any
	if err := json.Unmarshal(dump, &tables); err != nil {
		t.Fatalf("[%s] unmarshal dump: %v", runName, err)
	}
	// Visit ids are minted inside the fold and are not returned by any
	// authoring call; they are identified instead by the row they belong to,
	// which IS deterministic.
	for _, row := range tables["visit"] {
		vid, _ := row["visit_id"].(string)
		site, _ := row["site_id"].(string)
		person, _ := row["person_id"].(string)
		if vid == "" {
			continue
		}
		ids[vid] = "<visit:" + site + ":" + w5eLookup(ids, person) + ">"
	}
	var names []string
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		sb.WriteString("### " + name + "\n")
		var lines []string
		for _, row := range tables[name] {
			delete(row, "cargo_note") // the independent variable
			b, err := json.Marshal(row)
			if err != nil {
				t.Fatalf("[%s] marshal row of %s: %v", runName, name, err)
			}
			lines = append(lines, w5eSubstitute(string(b), ids))
		}
		sort.Strings(lines)
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}
	out := sb.String()
	w5eRequireNoMintedIDs(t, runName, "projection dump", out)
	return out
}

// w5eCanonicaliseJSON renders one read-path result comparable across runs.
//
// A top-level ARRAY is split, substituted element by element and SORTED. That
// is not a weakening: every read path here orders by a minted identity
// (TripStates by trip_id, TripOccupants by (trip_id, person_id)), so element
// ORDER is a function of crypto/rand and carries no information about cargo,
// while element CONTENT and MEMBERSHIP carry all of it and are compared
// exactly. Leaving the order in produced a failure on the first run of this
// test [MEASURED] — a red that named cargo and meant randomness, which is the
// least useful kind.
//
// The note's own text is deliberately NOT scrubbed from read-path results: if
// a read path echoes what the note SAYS, that is the finding, and it must
// surface as a difference rather than be canonicalised away.
func w5eCanonicaliseJSON(t *testing.T, runName string, b []byte, ids map[string]string) string {
	t.Helper()
	var elems []json.RawMessage
	if err := json.Unmarshal(b, &elems); err == nil {
		lines := make([]string, 0, len(elems))
		for _, e := range elems {
			lines = append(lines, w5eSubstitute(string(e), ids))
		}
		sort.Strings(lines)
		out := strings.Join(lines, "\n")
		w5eRequireNoMintedIDs(t, runName, "read-path result", out)
		return out
	}
	out := w5eSubstitute(string(b), ids)
	w5eRequireNoMintedIDs(t, runName, "read-path result", out)
	return out
}

func w5eLookup(ids map[string]string, id string) string {
	if v, ok := ids[id]; ok {
		return v
	}
	return id
}

// w5eSubstitute replaces every known identity, longest first so no token is a
// prefix of another.
func w5eSubstitute(s string, ids map[string]string) string {
	keys := make([]string, 0, len(ids))
	for k := range ids {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		s = strings.ReplaceAll(s, k, ids[k])
	}
	return s
}

// w5eRequireNoMintedIDs is rule 6 again: an unreplaced random identity would
// make every run differ for a reason that has nothing to do with cargo, and
// the test would "fail" while defending nothing.
func w5eRequireNoMintedIDs(t *testing.T, runName, what, s string) {
	t.Helper()
	if leftovers := w5eMintedID.FindAllString(s, -1); len(leftovers) != 0 {
		uniq := map[string]bool{}
		var list []string
		for _, l := range leftovers {
			if !uniq[l] {
				uniq[l] = true
				list = append(list, l)
			}
		}
		t.Fatalf("W5e-2 FIXTURE DEFECT [%s]: %s still carries un-canonicalised minted identities %v — every run would differ on random ids and the comparison would be meaningless", runName, what, list)
	}
}

func w5eFirstDifference(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return "first differing line " + w5eItoa(i+1) + ":\n  base:  " + la[i] + "\n  other: " + lb[i]
		}
	}
	return "identical for " + w5eItoa(w5eMin(len(la), len(lb))) + " lines, then one run has " + w5eItoa(len(la)) + " lines and the other " + w5eItoa(len(lb))
}

func w5eMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
